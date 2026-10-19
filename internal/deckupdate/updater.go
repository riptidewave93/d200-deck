package deckupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/ota"
)

// Update states, as Status reports them.
const (
	stateChecking   = "checking" // confirming a trial boot at startup
	stateIdle       = "idle"
	stateReceiving  = "receiving"
	stateConfirming = "confirming" // waiting for the key press
	stateInstalling = "installing"
	stateRebooting  = "rebooting"
	stateFailed     = "failed"
	stateCancelled  = "cancelled"
	// stateUnsupported: the system has no A/B slots to update
	stateUnsupported = "unsupported"
)

// Status is what /update/status reports.
type Status struct {
	State     string          `json:"state"`
	Step      string          `json:"step,omitempty"`
	Error     string          `json:"error,omitempty"`
	Update    *Slot           `json:"update,omitempty"` // the bundle being installed
	Slot      string          `json:"slot"`             // running
	Slots     map[string]Slot `json:"slots"`
	OTAResult string          `json:"ota_result,omitempty"` // of the last update: trial, ok, failed, rolled-back
	Last      *Report         `json:"last,omitempty"`
}

// Slot is the system in a slot.
type Slot struct {
	Version string `json:"version"`
	Flavour string `json:"flavour"`
	Build   string `json:"build"`
}

// Report is what the last install did.
type Report struct {
	Version string       `json:"version"`
	Slot    string       `json:"slot"`
	Time    time.Time    `json:"time"`
	Results []ota.Result `json:"results"`
	Error   string       `json:"error,omitempty"`
}

// saved is what survives a reboot, in statePath.
type saved struct {
	Slots map[string]Slot `json:"slots"`
	Last  *Report         `json:"last,omitempty"`
}

// screen shows the update on the D200 (deckd).
type screen interface {
	Confirm(ota.ConfirmRequest) (bool, error)
	Progress(step string)
	Close() error
}

type updater struct {
	dir       string // where bundles are received
	statePath string
	installed osrelease.Info
	slot      string // running
	env       ota.Env
	install   func(m *ota.Manifest, progress func(step string, n, of int)) ([]ota.Result, error)
	screen    func() (screen, error)
	reboot    func() error
	freeSpace func(dir string) (int64, error)
	healthy   func() bool

	mu    sync.Mutex
	st    Status
	saved saved
}

// start records the running slot and settles a trial boot.
func (u *updater) start() {
	u.mu.Lock()
	u.st.Slot = u.slot
	if u.saved.Slots == nil {
		u.saved.Slots = map[string]Slot{}
	}
	u.saved.Slots[u.slot] = Slot{u.installed.Version, u.installed.Flavour, u.installed.Build}
	u.saveLocked()
	u.mu.Unlock()
	u.set(stateChecking, "", nil)
	go func() {
		outcome, err := ota.Confirm(u.env, u.slot, u.healthy, u.reboot)
		if err != nil {
			log.Printf("confirming the boot: %v", err)
		} else if outcome != "" {
			log.Printf("update on slot %s: %s", u.slot, outcome)
		}
		u.readEnv()
		if outcome != ota.Rebooting {
			u.set(stateIdle, "", nil)
		}
	}()
}

func (u *updater) readEnv() {
	e, err := u.env.Get()
	if err != nil {
		log.Print(err)
		return
	}
	u.mu.Lock()
	u.st.OTAResult = e["ota_result"]
	u.mu.Unlock()
}

func (u *updater) load() {
	b, err := os.ReadFile(u.statePath)
	if err == nil {
		err = json.Unmarshal(b, &u.saved)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("%s: %v", u.statePath, err)
	}
}

func (u *updater) saveLocked() {
	b, _ := json.Marshal(u.saved)
	tmp := u.statePath + ".tmp"
	err := os.WriteFile(tmp, b, 0o644)
	if err == nil {
		err = os.Rename(tmp, u.statePath)
	}
	if err != nil {
		log.Printf("saving %s: %v", u.statePath, err)
	}
}

// set changes the state; err, if any, is reported with it.
func (u *updater) set(state, step string, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.st.State, u.st.Step, u.st.Error = state, step, ""
	if err != nil {
		u.st.Error = err.Error()
	}
}

func (u *updater) status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.st
	s.Slots, s.Last = map[string]Slot{}, u.saved.Last
	for k, v := range u.saved.Slots {
		s.Slots[k] = v
	}
	return s
}

// begin starts receiving a bundle, unless an update is under way.
func (u *updater) begin() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch u.st.State {
	case stateUnsupported:
		return errors.New("this system can't update itself: flash it over USB instead")
	case stateChecking:
		return errors.New("the D200 is still checking the last update; try again in a minute")
	case stateReceiving, stateConfirming, stateInstalling, stateRebooting:
		return errors.New("an update is already under way")
	}
	u.st = Status{State: stateReceiving, Slot: u.st.Slot, OTAResult: u.st.OTAResult}
	return nil
}

// spare is the room kept free on /data besides the bundle.
const spare = 1 << 20

// upload receives a bundle (PUT /update/bundle) and starts installing it.
func (u *updater) upload(w http.ResponseWriter, r *http.Request) {
	// a web page can't send this header to the device without its consent
	// (CORS), so other sites can't upload
	if r.Header.Get("X-D200-Update") == "" {
		http.Error(w, "missing the X-D200-Update header", http.StatusForbidden)
		return
	}
	if r.ContentLength <= 0 {
		http.Error(w, "the upload needs a Content-Length", http.StatusLengthRequired)
		return
	}
	if err := u.begin(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	m, code, err := u.receive(r)
	if err != nil {
		ota.Clear(u.dir)
		u.set(stateFailed, "", err)
		http.Error(w, err.Error(), code)
		return
	}
	u.mu.Lock()
	u.st.Update = &Slot{m.Version, m.Flavour, strconv.FormatInt(m.BuildID, 10)}
	u.mu.Unlock()
	u.set(stateConfirming, "", nil)
	go u.run(m)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(u.status())
}

func (u *updater) receive(r *http.Request) (*ota.Manifest, int, error) {
	free, err := u.freeSpace(u.dir)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if r.ContentLength > free-spare {
		return nil, http.StatusInsufficientStorage, fmt.Errorf("not enough room: %d MB free", free>>20)
	}
	if err := ota.Clear(u.dir); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	m, err := ota.Receive(io.LimitReader(r.Body, r.ContentLength), u.dir)
	if err == nil {
		err = m.CheckFlavour(u.installed.Flavour)
	}
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	return m, 0, nil
}

// failDelay is how long a failure stays on the D200's screen; rebootDelay
// how long "rebooting" shows before it does.
var failDelay, rebootDelay = 5 * time.Second, time.Second

// run asks for the key press, installs and reboots.
func (u *updater) run(m *ota.Manifest) {
	defer func() {
		if u.status().State != stateRebooting {
			ota.Clear(u.dir)
		}
	}()
	sc, err := u.screen()
	if err != nil {
		u.set(stateFailed, "", fmt.Errorf("can't ask for the key press on the D200: %w", err))
		return
	}
	defer sc.Close()
	installed, _ := strconv.ParseInt(u.installed.Build, 10, 64)
	ok, err := sc.Confirm(ota.ConfirmRequest{Version: m.Version, Flavour: m.Flavour, Older: m.BuildID <= installed})
	if err != nil || !ok {
		u.set(stateCancelled, "", err)
		return
	}

	u.set(stateInstalling, "", nil)
	results, err := u.install(m, func(step string, n, of int) {
		text := fmt.Sprintf("%s (%d/%d)", step, n, of)
		u.set(stateInstalling, text, nil)
		sc.Progress(text)
	})
	slot := ota.Other(u.slot)
	report := &Report{Version: m.Version, Slot: slot, Time: time.Now().UTC(), Results: results}
	if err != nil {
		report.Error = err.Error()
	}
	u.mu.Lock()
	u.saved.Last = report
	if err == nil {
		u.saved.Slots[slot] = Slot{m.Version, m.Flavour, strconv.FormatInt(m.BuildID, 10)}
	}
	u.saveLocked()
	u.mu.Unlock()
	if err != nil {
		log.Printf("update %s: %v", m.Version, err)
		u.set(stateFailed, "", err)
		sc.Progress("Failed")
		time.Sleep(failDelay)
		return
	}
	log.Printf("update %s written to slot %s; rebooting", m.Version, slot)
	u.set(stateRebooting, "", nil)
	sc.Progress("Restarting")
	time.Sleep(rebootDelay) // for the page to see it
	if err := u.reboot(); err != nil {
		u.set(stateFailed, "", fmt.Errorf("rebooting: %w", err))
	}
}

func (u *updater) showStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(u.status())
}

// deckdScreen talks to deckd's update screen.
type deckdScreen struct {
	c   net.Conn
	enc *json.Encoder
	dec *json.Decoder
}

func dialDeckd(path string) (screen, error) {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &deckdScreen{c, json.NewEncoder(c), json.NewDecoder(c)}, nil
}

func (s *deckdScreen) Confirm(req ota.ConfirmRequest) (bool, error) {
	if err := s.enc.Encode(ota.UIRequest{Confirm: &req}); err != nil {
		return false, err
	}
	s.c.SetReadDeadline(time.Now().Add(3 * time.Minute)) // deckd gives up first
	var r ota.UIReply
	if err := s.dec.Decode(&r); err != nil {
		return false, err
	}
	return r.OK, nil
}

func (s *deckdScreen) Progress(step string) { s.enc.Encode(ota.UIRequest{Progress: step}) }
func (s *deckdScreen) Close() error         { return s.c.Close() }

// deckdHealthy waits up to wait for deckd to have drawn its first page
// (ready holds its PID), then checks it's still running after settle.
func deckdHealthy(ready string, wait, settle time.Duration) bool {
	alive := func(pid int) bool {
		_, err := os.Stat("/proc/" + strconv.Itoa(pid))
		return pid > 0 && err == nil
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(time.Second) {
		b, _ := os.ReadFile(ready)
		if pid, _ := strconv.Atoi(string(b)); alive(pid) {
			time.Sleep(settle)
			return alive(pid)
		}
	}
	return false
}

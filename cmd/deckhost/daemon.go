package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidraw"
	"github.com/riptidewave93/d200-deck/netid"
	"github.com/riptidewave93/d200-deck/proto"
)

// page 0xffd2).

// Device is one D200 as the daemon sees it.
type Device struct {
	Path  string       `json:"path"`
	Hello *proto.Hello `json:"hello,omitempty"` // set while connected
	Error string       `json:"error,omitempty"` // why it isn't connected
	// OSUpdate is a newer OS release than the device's, if there's one
	OSUpdate *Release `json:"os_update,omitempty"`
	// NeedsHost is set if the device's software is too new for deckhost
	NeedsHost bool `json:"needs_host,omitempty"`
	busy      bool // a session is running
}

// Status is what the daemon reports to the tray: every D200 plugged in.
type Status struct {
	Devices []Device    `json:"devices"`
	Apps    []AppStatus `json:"apps,omitempty"` // the app integrations that are set up
	// HostUpdate is a newer deckhost release, if there's one or a device
	// needs one (then its Version may be unknown)
	HostUpdate *Release `json:"host_update,omitempty"`
}

// Protocols deckhost can't talk.
var (
	errDeviceOld = errors.New("the device's software is too old: update it")
	errHostOld   = errors.New("the device's software is newer: update deckhost")
)

// Connected are the devices with a working session.
func (s Status) Connected() []Device {
	var c []Device
	for _, d := range s.Devices {
		if d.Hello != nil {
			c = append(c, d)
		}
	}
	return c
}

// UpdateURL is the update page of the connected device whose serial ends in
// serial, or of the only one connected if serial is empty.
func (s Status) UpdateURL(serial string) (string, error) {
	var found []Device
	for _, d := range s.Connected() {
		if strings.HasSuffix(strings.ToLower(d.Hello.Serial), strings.ToLower(serial)) {
			found = append(found, d)
		}
	}
	switch {
	case len(found) == 0 && serial == "":
		return "", errors.New("no D200 connected")
	case len(found) == 0:
		return "", fmt.Errorf("no D200 %s connected", serial)
	case len(found) > 1:
		return "", errors.New("several D200s connected: name one by its serial")
	}
	if ip := found[0].Hello.IPv4; ip != "" {
		return "http://" + ip + "/update", nil
	}
	id, err := netid.For(found[0].Hello.Serial) // a deckd too old to say
	if err != nil {
		return "", fmt.Errorf("D200 %q: %w", found[0].Hello.Serial, err)
	}
	return id.UpdateURL(), nil
}

// daemon runs one session per plugged-in D200.
type daemon struct {
	mu      sync.Mutex
	devices map[string]*Device // by hidraw path
	id      string             // this daemon's action host ID
	lock    *os.File           // the single-instance lock, held while running
	find    func() []string
	open    func(path string) (proto.Transport, error)
	apps    map[string]app // by name, as in app actions
	// state is the names that are on (e.g. discord.mute), which the decks
	// show; known is every name ever set. Each session is told when it
	// changes.
	state    map[string]bool
	known    map[string]bool
	looks    map[string]hostLook // by action state name
	watchers map[chan struct{}]bool
	os       *osUpdates // nil: not looked for
}

func newDaemon() *daemon {
	var id [8]byte
	rand.Read(id[:])
	d := &daemon{
		id:       hex.EncodeToString(id[:]),
		devices:  map[string]*Device{},
		find:     func() []string { return hidraw.FindAll(proto.RawDescriptor) },
		open:     func(path string) (proto.Transport, error) { return hidraw.Open(path) },
		state:    map[string]bool{},
		known:    map[string]bool{},
		looks:    map[string]hostLook{},
		watchers: map[chan struct{}]bool{},
		os:       newOSUpdates(notify),
	}
	d.apps = map[string]app{
		deck.AppDiscord: newDiscordLink(discordLogin, d.setState),
		deck.AppOBS:     newOBSLink(obsDial, d.setState),
		deck.AppMedia:   newMediaLink(openMedia, d.setState, d.setLook),
	}
	return d
}

// setState merges s into the decks' state and tells the sessions if it
// changed.
func (d *daemon) setState(s map[string]bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	changed := false
	for k, on := range s {
		d.known[k] = true
		if on != d.state[k] {
			changed = true
			if on {
				d.state[k] = true
			} else {
				delete(d.state, k)
			}
		}
	}
	if changed {
		d.notifyLocked()
	}
}

// notifyLocked tells the sessions the state or looks changed.
func (d *daemon) notifyLocked() {
	for ch := range d.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// watchState returns a channel told about state changes, and a function to
// stop it.
func (d *daemon) watchState() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	d.mu.Lock()
	d.watchers[ch] = true
	d.mu.Unlock()
	return ch, func() {
		d.mu.Lock()
		delete(d.watchers, ch)
		d.mu.Unlock()
	}
}

// stateMessages are the state requests that bring a device from sent, the
// names it was last told are on (nil if it was told nothing), to now: the
// names on, and those now off. Each fits in one request. on is the names now
// on.
func (d *daemon) stateMessages(sent map[string]bool) (msgs [][]byte, on map[string]bool) {
	d.mu.Lock()
	on = maps.Clone(d.state)
	off := sent
	if off == nil {
		off = d.known
	}
	change := map[string]bool{}
	for k := range off {
		if !on[k] {
			change[k] = false
		}
	}
	d.mu.Unlock()
	maps.Copy(change, on)
	msg := proto.State{Host: d.id, State: map[string]bool{}}
	for _, k := range slices.Sorted(maps.Keys(change)) {
		msg.State[k] = change[k]
		if b, _ := json.Marshal(msg); len(b) > proto.MaxRequest && len(msg.State) > 1 {
			delete(msg.State, k)
			b, _ = json.Marshal(msg)
			msgs = append(msgs, b)
			msg.State = map[string]bool{k: change[k]}
		}
	}
	if len(msg.State) > 0 {
		b, _ := json.Marshal(msg)
		msgs = append(msgs, b)
	}
	return msgs, on
}

func (d *daemon) getStatus() Status {
	var s Status
	// before taking d.mu: apps hold their own lock while setting state
	for _, a := range d.apps {
		if st := a.statusText(); st != "" {
			s.Apps = append(s.Apps, AppStatus{a.title(), st})
		}
	}
	sort.Slice(s.Apps, func(i, j int) bool { return s.Apps[i].Title < s.Apps[j].Title })
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.os != nil {
		s.HostUpdate = d.os.hostUpdate()
	}
	for _, dev := range d.devices {
		c := *dev
		if c.Hello != nil && d.os != nil {
			c.OSUpdate = d.os.update(*c.Hello)
		}
		if c.NeedsHost && s.HostUpdate == nil {
			s.HostUpdate = &Release{URL: deck.RepoURL + "/releases/latest"}
		}
		s.Devices = append(s.Devices, c)
	}
	sort.Slice(s.Devices, func(i, j int) bool { return s.Devices[i].Path < s.Devices[j].Path })
	return s
}

func runDaemon() {
	logToFile()
	d := newDaemon()
	ctl, err := listenControl(d)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("deckhost %s", version)
	for _, a := range d.apps {
		go a.run(nil)
	}
	go d.os.run(d.hellos)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		ctl.Close()
		os.Exit(0)
	}()
	for {
		d.scan()
		time.Sleep(2 * time.Second)
	}
}

// scan starts a session for each plugged-in D200 that has none, and forgets
// unplugged ones.
func (d *daemon) scan() {
	found := map[string]bool{}
	for _, path := range d.find() {
		found[path] = true
		d.mu.Lock()
		dev, ok := d.devices[path]
		if !ok {
			dev = &Device{Path: path}
			d.devices[path] = dev
		}
		start := !dev.busy
		dev.busy = true
		d.mu.Unlock()
		if start {
			go d.run(path)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for path, dev := range d.devices {
		if !found[path] && !dev.busy {
			delete(d.devices, path)
		}
	}
}

// run holds one session with the device at path.
func (d *daemon) run(path string) {
	err := d.session(path)
	d.mu.Lock()
	defer d.mu.Unlock()
	dev := d.devices[path]
	dev.busy = false
	dev.Hello = nil
	dev.NeedsHost = errors.Is(err, errHostOld)
	if msg := err.Error(); msg != dev.Error {
		log.Printf("%s: %s", path, msg)
		dev.Error = msg
	}
}

func (d *daemon) connected(path string, hello proto.Hello) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dev, ok := d.devices[path]; ok {
		dev.Hello = &hello
		dev.Error = ""
	}
	log.Printf("%s: connected to D200 %s (OS %s, deckd %s)", path, hello.Serial, hello.OS.Version, hello.Firmware)
	if d.os != nil {
		go d.os.check([]proto.Hello{hello})
	}
}

// hellos are the connected devices' hellos.
func (d *daemon) hellos() []proto.Hello {
	d.mu.Lock()
	defer d.mu.Unlock()
	var hs []proto.Hello
	for _, dev := range d.devices {
		if dev.Hello != nil {
			hs = append(hs, *dev.Hello)
		}
	}
	return hs
}

// session talks to the device at path until it goes away.
func (d *daemon) session(path string) error {
	t, err := d.open(path)
	if err != nil {
		return err
	}
	c := proto.NewConn(t)
	defer c.Close()
	return d.serve(c, path)
}

// setHostOS tells a device that takes it which OS the PC runs, if that
// changed: keys with Mac combos send them to a Mac.
func setHostOS(do func(byte, []byte) ([]byte, error), hello proto.Hello, goos string) error {
	if hello.Features == nil || !slices.Contains(hello.Features.Settings, "host_os") {
		return nil
	}
	b, err := do(proto.CmdSettings, nil)
	if err != nil {
		return err
	}
	var s proto.Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v := proto.HostOther
	if goos == "darwin" {
		v = proto.HostMac
	}
	if s.HostOS != nil && *s.HostOS == v {
		return nil
	}
	b, _ = json.Marshal(proto.Settings{HostOS: &v})
	_, err = do(proto.CmdSettings, b)
	return err
}

// serve runs PC actions for the device on c until the connection fails.
func (d *daemon) serve(c *proto.Conn, path string) error {
	b, err := c.Do(proto.CmdHello, nil)
	if err != nil {
		return err
	}
	var hello proto.Hello
	if err := json.Unmarshal(b, &hello); err != nil {
		return fmt.Errorf("bad hello: %w", err)
	}
	switch {
	case hello.Protocol < proto.MinVersion:
		return errDeviceOld
	case hello.Protocol > proto.Version:
		return errHostOld
	}
	reply, err := c.Do(proto.CmdActionHost, d.heartbeat())
	if err != nil {
		return err
	}
	d.connected(path, hello)
	if err := setHostOS(c.Do, hello, runtime.GOOS); err != nil {
		log.Printf("%s: host os: %v", path, err)
	}
	// the looks the device shows: nil while it can't say, or doesn't take
	// them (an older deckd)
	looks := deviceLooks(reply)

	// the deck takes state only from its action host: send it after each
	// heartbeat (deckd may have restarted and lost it) and when it changes
	stateChanged, unwatch := d.watchState()
	defer unwatch()
	sendState := true
	var sent map[string]bool // the names the device was last told are on
	heartbeat := time.NewTicker(heartbeatPeriod)
	defer heartbeat.Stop()
	// the device has no battery-backed clock
	sendTime := func() {
		b, _ := json.Marshal(proto.Now())
		c.Do(proto.CmdTime, b) // an older deckd doesn't know it
	}
	sendTime()
	clock := time.NewTicker(proto.TimePeriod)
	defer clock.Stop()
	missed := 0
	results := make(chan proto.Result, 16)
	act := func(a proto.ActionEvent) {
		err := d.runAction(a.Action, a.Release)
		if err != nil {
			log.Printf("key %d: %v", a.Key, err)
		}
		if a.Release {
			return // the press has the result
		}
		r := proto.Result{ID: a.ID}
		if err != nil {
			r.Error = shorten(err.Error(), maxResultError)
		}
		select {
		case results <- r:
		case <-c.Done():
		}
	}
	// each app's keys go in order, so e.g. a quick push-to-talk tap ends
	// muted
	queues := map[string]chan proto.ActionEvent{}
	queue := func(name string) chan proto.ActionEvent {
		q, ok := queues[name]
		if !ok {
			q = make(chan proto.ActionEvent, 16)
			queues[name] = q
			go func() {
				for {
					select {
					case a := <-q:
						act(a)
					case <-c.Done():
						return
					}
				}
			}()
		}
		return q
	}
	for {
		if sendState {
			msgs, on := d.stateMessages(sent)
			sendState = false
			for _, m := range msgs {
				if _, err := c.Do(proto.CmdState, m); err != nil {
					sendState = true
					break
				}
			}
			if !sendState {
				sent = on
			}
			if looks != nil {
				if err := d.syncLooks(c, looks); proto.IsUnknownCmd(err) {
					looks = nil
				} else if err != nil {
					log.Printf("%s: looks: %v", path, err)
					sendState = true // try again
				}
			}
		}
		select {
		case <-c.Done():
			return fmt.Errorf("disconnected: %w", c.Err())
		case <-stateChanged:
			sendState = true
		case <-clock.C:
			sendTime()
		case <-heartbeat.C:
			// another client's request can displace ours: allow one miss
			if reply, err := c.Do(proto.CmdActionHost, d.heartbeat()); err == nil {
				missed, sendState = 0, true
				// what it shows now: a restarted deckd has none, and may be
				// newer or older than before
				looks = deviceLooks(reply)
			} else if missed++; missed > 1 {
				return err
			}
		case ev := <-c.Events():
			if ev.Code != proto.EventAction {
				continue
			}
			var a proto.ActionEvent
			if err := json.Unmarshal(ev.Payload, &a); err != nil {
				log.Printf("bad action event: %v", err)
				continue
			}
			switch {
			case a.Host != d.id:
				// another deckhost's
			case a.Action.Type == deck.ActionApp:
				select {
				case queue(a.Action.App) <- a:
				default:
					log.Printf("key %d: too many %s keys at once", a.Key, a.Action.App)
				}
			case !a.Release: // none of the others need the release
				go act(a)
			}
		case r := <-results:
			b, _ := json.Marshal(r)
			if _, err := c.Do(proto.CmdResult, b); err != nil {
				return err
			}
		}
	}
}

// heartbeatPeriod is how often deckhost tells the device it's there.
var heartbeatPeriod = proto.ActionHostPeriod

// maxResultError bounds an error sent back to the device, which must fit in
// one request.
const maxResultError = 200

// shorten cuts s to at most n bytes, on a rune boundary.
func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// heartbeat says deckhost runs PC actions, and what it can do: the device
// tells the configuration website.
func (d *daemon) heartbeat() []byte {
	h := proto.ActionHost{ID: d.id, Name: "deckhost", Version: version, OS: runtime.GOOS}
	for _, name := range slices.Sorted(maps.Keys(d.apps)) {
		a := d.apps[name]
		h.Apps = append(h.Apps, proto.HostApp{Name: name, Title: a.title(), Status: a.statusText(), Ops: a.ops()})
	}
	b, _ := json.Marshal(h)
	if len(b) > proto.MaxRequest {
		for i := range h.Apps {
			h.Apps[i].Ops = nil
		}
		b, _ = json.Marshal(h)
	}
	return b
}

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
	busy  bool         // a session is running
}

// Status is what the daemon reports to the tray: every D200 plugged in.
type Status struct {
	Devices []Device    `json:"devices"`
	Apps    []AppStatus `json:"apps,omitempty"` // the app integrations that are set up
}

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
	// state is what the decks show (e.g. discord.mute); each session is
	// told on its channel when it changes
	state    map[string]bool
	watchers map[chan struct{}]bool
}

func newDaemon() *daemon {
	var id [8]byte
	rand.Read(id[:])
	d := &daemon{
		id:       hex.EncodeToString(id[:]),
		devices:  map[string]*Device{},
		find:     func() []string { return hidraw.FindAll(proto.RawDescriptor) },
		open:     func(path string) (proto.Transport, error) { return hidraw.Open(path) },
		watchers: map[chan struct{}]bool{},
	}
	d.apps = map[string]app{deck.AppDiscord: newDiscordLink(discordLogin, d.setState)}
	return d
}

// setState merges s into the decks' state and tells the sessions if it
// changed.
func (d *daemon) setState(s map[string]bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	changed := false
	for k, v := range s {
		if old, ok := d.state[k]; !ok || old != v {
			if d.state == nil {
				d.state = map[string]bool{}
			}
			d.state[k] = v
			changed = true
		}
	}
	if changed {
		for ch := range d.watchers {
			select {
			case ch <- struct{}{}:
			default:
			}
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

func (d *daemon) stateMessage() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, _ := json.Marshal(proto.State{Host: d.id, State: d.state})
	return b
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
	for _, dev := range d.devices {
		s.Devices = append(s.Devices, *dev)
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
	if hello.Protocol != proto.Version {
		return fmt.Errorf("device speaks protocol %d, deckhost %d; update the older one", hello.Protocol, proto.Version)
	}
	if _, err := c.Do(proto.CmdActionHost, d.heartbeat()); err != nil {
		return err
	}
	d.connected(path, hello)

	// the deck takes state only from its action host: send it after each
	// heartbeat (deckd may have restarted and lost it) and when it changes
	stateChanged, unwatch := d.watchState()
	defer unwatch()
	sendState := true
	heartbeat := time.NewTicker(heartbeatPeriod)
	defer heartbeat.Stop()
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
			_, err := c.Do(proto.CmdState, d.stateMessage())
			sendState = err != nil
		}
		select {
		case <-c.Done():
			return fmt.Errorf("disconnected: %w", c.Err())
		case <-stateChanged:
			sendState = true
		case <-heartbeat.C:
			// another client's request can displace ours: allow one miss
			if _, err := c.Do(proto.CmdActionHost, d.heartbeat()); err == nil {
				missed, sendState = 0, true
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

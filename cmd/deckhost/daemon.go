package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

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
	Devices []Device `json:"devices"`
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
	id, err := netid.For(found[0].Hello.Serial)
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
}

func newDaemon() *daemon {
	var id [8]byte
	rand.Read(id[:])
	return &daemon{
		id:      hex.EncodeToString(id[:]),
		devices: map[string]*Device{},
		find:    func() []string { return hidraw.FindAll(proto.RawDescriptor) },
		open:    func(path string) (proto.Transport, error) { return hidraw.Open(path) },
	}
}

func (d *daemon) getStatus() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	var s Status
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
	beat, _ := json.Marshal(proto.ActionHost{ID: d.id})
	if _, err := c.Do(proto.CmdActionHost, beat); err != nil {
		return err
	}
	d.connected(path, hello)

	heartbeat := time.NewTicker(heartbeatPeriod)
	defer heartbeat.Stop()
	missed := 0
	results := make(chan proto.Result, 16)
	for {
		select {
		case <-c.Done():
			return fmt.Errorf("disconnected: %w", c.Err())
		case <-heartbeat.C:
			// another client's request can displace ours: allow one miss
			if _, err := c.Do(proto.CmdActionHost, beat); err == nil {
				missed = 0
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
			if a.Host != d.id || a.Release {
				continue // another deckhost's, or a release (none of ours need it yet)
			}
			go func() {
				r := proto.Result{ID: a.ID}
				if err := run(a.Action); err != nil {
					log.Printf("key %d: %v", a.Key, err)
					r.Error = shorten(err.Error(), maxResultError)
				}
				select {
				case results <- r:
				case <-c.Done():
				}
			}()
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

package deckd

import (
	"encoding/json"
	"log"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

// pcActionTimeout is how long the action host has to report a result.
var pcActionTimeout = 15 * time.Second

// pcActions tracks the action host (deckhost), the PC actions sent to it and
// the state it set.
type pcActions struct {
	mu      sync.Mutex
	host    string // the host heard from last
	info    proto.ActionHost
	beat    time.Time // and when
	state   map[string]bool
	looks   map[string]hostLook // by action state name
	upload  *lookUpload
	nextID  uint32
	pending map[uint32]pending
	// releases are the key releases still to send to the action host, by
	// key. One that fails goes with the next heartbeat.
	releases map[int]proto.ActionEvent
	// fail shows that the action of key failed
	fail func(key int)
	// changed is called when the state may have changed
	changed func()
}

type pending struct {
	key   int
	timer *time.Timer
}

// heartbeat makes host the action host. A new host starts with no state.
func (p *pcActions) heartbeat(h proto.ActionHost) {
	host := h.ID
	p.mu.Lock()
	defer p.mu.Unlock()
	p.info = h
	if host != p.host || !p.aliveLocked() {
		p.host = host
		p.upload = nil
		if len(p.state) > 0 || len(p.looks) > 0 {
			p.state, p.looks = nil, nil
			p.notify()
		}
	}
	p.beat = time.Now()
}

func (p *pcActions) aliveLocked() bool {
	return !p.beat.IsZero() && time.Since(p.beat) < proto.ActionHostTimeout
}

// hostInfo describes the action host.
func (p *pcActions) hostInfo() proto.HostInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.aliveLocked() {
		return proto.HostInfo{}
	}
	return proto.HostInfo{Connected: true, ActionHost: p.info, State: maps.Clone(p.state)}
}

// hostAlive returns the action host, if one is connected.
func (p *pcActions) hostAlive() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.host, p.aliveLocked()
}

// setState takes state from the current action host.
func (p *pcActions) setState(s proto.State) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.Host != p.host || !p.aliveLocked() {
		return
	}
	if p.state == nil {
		p.state = map[string]bool{}
	}
	changed := false
	for k, v := range s.State {
		changed = changed || p.state[k] != v
		p.state[k] = v
	}
	if changed { // the host resends its state with every heartbeat
		p.notify()
	}
}

// ready reports whether the action host can run a now.
func (p *pcActions) ready(a deck.Action) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.aliveLocked() && (a.Ready() == "" || p.state[a.Ready()])
}

// getState is the value of state name; false without an action host.
func (p *pcActions) getState(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.aliveLocked() && p.state[name]
}

func (p *pcActions) notify() {
	if p.changed != nil {
		go p.changed()
	}
}

// start registers an action for key and returns its ID.
func (p *pcActions) start(key int) uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == nil {
		p.pending = map[uint32]pending{}
	}
	delete(p.releases, key) // the new press's release replaces it
	p.nextID++
	id := p.nextID
	p.pending[id] = pending{key, time.AfterFunc(pcActionTimeout, func() {
		if key, ok := p.finish(id); ok {
			log.Printf("key %d: no result from the PC", key)
			p.fail(key)
		}
	})}
	return id
}

// finish forgets action id; ok is false if it had already finished.
func (p *pcActions) finish(id uint32) (key int, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.pending[id]
	if ok {
		a.timer.Stop()
		delete(p.pending, id)
	}
	return a.key, ok
}

func (p *pcActions) result(r proto.Result) {
	key, ok := p.finish(r.ID)
	if ok && r.Error != "" {
		log.Printf("key %d: %s", key, r.Error)
		p.fail(key)
	}
}

// sendPC asks the action host to run a, and returns the action's ID (0 if
// there's no host) and the host. It flashes key i red if no host is
// connected or the action fails.
func (d *deckd) sendPC(i int, a deck.Action) (uint32, string) {
	host, ok := d.pc.hostAlive()
	if !ok {
		log.Printf("key %d: %s needs deckhost on the PC", i, a.Type)
		d.flashError(i)
		return 0, ""
	}
	id := d.pc.start(i)
	ev, _ := json.Marshal(proto.ActionEvent{ID: id, Host: host, Key: i, Action: a.ForPC()})
	go func() {
		if err := d.ch.send(proto.EventSeq, proto.EventAction, ev); err != nil {
			if _, ok := d.pc.finish(id); ok {
				log.Printf("key %d: %v", i, err)
				d.pc.fail(i)
			}
		}
	}()
	return id, host
}

// releasePC tells the action host that key i, held with h, was let go. A
// press sent always gets its release, even while the host seems gone, so
// e.g. push-to-talk doesn't stay on.
func (d *deckd) releasePC(i int, h *held) {
	if h.pcID == 0 {
		return
	}
	ev := proto.ActionEvent{ID: h.pcID, Host: h.pcHost, Key: i, Release: true, Action: h.action.ForPC()}
	d.pc.addRelease(ev)
	go d.sendReleases([]proto.ActionEvent{ev})
}

// sendReleases sends key releases, keeping those that fail for the next
// heartbeat.
func (d *deckd) sendReleases(evs []proto.ActionEvent) {
	for _, ev := range evs {
		b, _ := json.Marshal(ev)
		if err := d.ch.send(proto.EventSeq, proto.EventAction, b); err != nil {
			log.Printf("key %d release: %v", ev.Key, err)
			continue
		}
		d.pc.released(ev)
	}
}

func (p *pcActions) addRelease(ev proto.ActionEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.releases == nil {
		p.releases = map[int]proto.ActionEvent{}
	}
	p.releases[ev.Key] = ev
}

// unsentReleases are the key releases still to send.
func (p *pcActions) unsentReleases() []proto.ActionEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Collect(maps.Values(p.releases))
}

func (p *pcActions) released(ev proto.ActionEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.releases[ev.Key].ID == ev.ID {
		delete(p.releases, ev.Key)
	}
}

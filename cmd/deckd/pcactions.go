package main

import (
	"encoding/json"
	"log"
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
	host    string    // the host heard from last
	beat    time.Time // and when
	state   map[string]bool
	nextID  uint32
	pending map[uint32]pending
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
func (p *pcActions) heartbeat(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if host != p.host || !p.aliveLocked() {
		p.host = host
		if len(p.state) > 0 {
			p.state = nil
			p.notify()
		}
	}
	p.beat = time.Now()
}

func (p *pcActions) aliveLocked() bool {
	return !p.beat.IsZero() && time.Since(p.beat) < proto.ActionHostTimeout
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
	for k, v := range s.State {
		p.state[k] = v
	}
	p.notify()
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
// there's no host). It flashes key i red if no host is connected or the
// action fails.
func (d *deckd) sendPC(i int, a deck.Action) uint32 {
	host, ok := d.pc.hostAlive()
	if !ok {
		log.Printf("key %d: %s needs deckhost on the PC", i, a.Type)
		d.flashError(i)
		return 0
	}
	id := d.pc.start(i)
	ev, _ := json.Marshal(proto.ActionEvent{ID: id, Host: host, Key: i, Action: a})
	go func() {
		if err := d.ch.send(proto.EventSeq, proto.EventAction, ev); err != nil {
			if _, ok := d.pc.finish(id); ok {
				log.Printf("key %d: %v", i, err)
				d.pc.fail(i)
			}
		}
	}()
	return id
}

// releasePC tells the action host key i, whose action id was a, was let go.
func (d *deckd) releasePC(i int, a deck.Action, id uint32) {
	host, ok := d.pc.hostAlive()
	if id == 0 || !ok {
		return
	}
	ev, _ := json.Marshal(proto.ActionEvent{ID: id, Host: host, Key: i, Release: true, Action: a})
	go func() {
		if err := d.ch.send(proto.EventSeq, proto.EventAction, ev); err != nil {
			log.Printf("key %d release: %v", i, err)
		}
	}()
}

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

// pcActions tracks the PC actions sent to the action host (deckhost).
type pcActions struct {
	mu      sync.Mutex
	host    time.Time // last heartbeat
	nextID  uint32
	pending map[uint32]pending
	// fail shows that the action of key failed
	fail func(key int)
}

type pending struct {
	key   int
	timer *time.Timer
}

func (p *pcActions) heartbeat() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.host = time.Now()
}

func (p *pcActions) hostAlive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.host.IsZero() && time.Since(p.host) < proto.ActionHostTimeout
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

// sendPC asks the action host to run a. It flashes key i red if no host is
// connected or the action fails.
func (d *deckd) sendPC(i int, a deck.Action) {
	if !d.pc.hostAlive() {
		log.Printf("key %d: %s needs deckhost on the PC", i, a.Type)
		d.flashError(i)
		return
	}
	id := d.pc.start(i)
	ev, _ := json.Marshal(proto.ActionEvent{ID: id, Key: i, Action: a})
	go func() {
		if err := d.ch.send(proto.EventSeq, proto.EventAction, ev); err != nil {
			if _, ok := d.pc.finish(id); ok {
				log.Printf("key %d: %v", i, err)
				d.pc.fail(i)
			}
		}
	}()
}

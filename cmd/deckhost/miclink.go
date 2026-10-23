package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
)

// micBackend mutes the PC's default microphone (mic_linux.go,
// mic_windows.go, mic_darwin.go).
type micBackend interface {
	muted() (bool, error)
	setMuted(on bool) error
	close()
}

// errNoMic means the PC has no microphone.
var errNoMic = errors.New("no microphone")

// micLink follows the mic's mute and runs mic actions. Polling is simpler
// than each OS's notifications.
type micLink struct {
	open    func() (micBackend, error)
	publish func(map[string]bool)
	poll    time.Duration
	retry   time.Duration

	mu      sync.Mutex
	backend micBackend
	status  string
}

func newMicLink(open func() (micBackend, error), publish func(map[string]bool)) *micLink {
	return &micLink{open: open, publish: publish, poll: time.Second, retry: 5 * time.Second}
}

func (l *micLink) title() string { return "Mic" }

func (l *micLink) ops() []string { return deck.MicOps }

func (l *micLink) statusText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

// run follows the mic until stop is closed.
func (l *micLink) run(stop <-chan struct{}) {
	var last string
	for {
		b, err := l.open()
		if errors.Is(err, errors.ErrUnsupported) {
			return
		}
		if err == nil {
			err = l.follow(b, stop)
			b.close()
		}
		if err == nil {
			return
		}
		if err.Error() != last && !errors.Is(err, errNoMic) {
			log.Printf("mic: %v", err)
		}
		last = err.Error()
		l.update(nil, false, err)
		select {
		case <-stop:
			return
		case <-time.After(l.retry):
		}
	}
}

// follow polls b until stop is closed (nil) or b fails.
func (l *micLink) follow(b micBackend, stop <-chan struct{}) error {
	t := time.NewTicker(l.poll)
	defer t.Stop()
	for {
		m, err := b.muted()
		if err != nil {
			return err
		}
		l.update(b, m, nil)
		select {
		case <-stop:
			return nil
		case <-t.C:
		}
	}
}

// update takes the mic's new state, or err if there's none, and publishes it.
func (l *micLink) update(b micBackend, muted bool, err error) {
	l.mu.Lock()
	l.backend = b
	switch {
	case errors.Is(err, errNoMic):
		l.status = "no microphone"
	case b == nil:
		l.status = "not available"
	case muted:
		l.status = "muted"
	default:
		l.status = "on"
	}
	l.mu.Unlock()
	l.publish(map[string]bool{"mic.ready": b != nil, deck.AppMic + "." + deck.MicMute: muted})
}

// do runs mic operation op for a key's press, or its release.
func (l *micLink) do(op string, _ json.RawMessage, release bool) error {
	if !slices.Contains(deck.MicOps, op) {
		return fmt.Errorf("mic has no %q op", op)
	}
	if release && op != deck.MicPushToTalk {
		return nil
	}
	l.mu.Lock()
	b, status := l.backend, l.status
	l.mu.Unlock()
	if b == nil {
		return errors.New("mic: " + status)
	}
	on := release
	if op == deck.MicMute {
		m, err := b.muted() // the polled state may be a second old
		if err != nil {
			return fmt.Errorf("mic: %w", err)
		}
		on = !m
	}
	if err := b.setMuted(on); err != nil {
		return fmt.Errorf("mic: %w", err)
	}
	l.mu.Lock()
	current := l.backend == b
	l.mu.Unlock()
	if current {
		l.update(b, on, nil)
	}
	return nil
}

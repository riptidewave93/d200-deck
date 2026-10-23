package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
)

// fakeMic is a mic backend to mute, or to fail.
type fakeMic struct {
	mu    sync.Mutex
	mute  bool
	fail  error
	calls []bool // setMuted's
}

func (f *fakeMic) muted() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mute, f.fail
}

func (f *fakeMic) setMuted(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, on)
	f.mute = on
	return f.fail
}

func (f *fakeMic) close() {}

func (f *fakeMic) set(mute bool, fail error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mute, f.fail = mute, fail
}

func TestMic(t *testing.T) {
	fake := &fakeMic{fail: errNoMic}
	var mu sync.Mutex
	state := map[string]bool{}
	l := newMicLink(func() (micBackend, error) { return fake, nil }, func(s map[string]bool) {
		mu.Lock()
		defer mu.Unlock()
		for k, v := range s {
			state[k] = v
		}
	})
	l.poll, l.retry = 5*time.Millisecond, 5*time.Millisecond
	is := func(name string, on bool) func() bool {
		return func() bool {
			mu.Lock()
			defer mu.Unlock()
			return state[name] == on
		}
	}
	stop := make(chan struct{})
	defer close(stop)
	go l.run(stop)

	waitFor(t, "no microphone", func() bool { return l.statusText() == "no microphone" })
	if err := l.do(deck.MicMute, nil, false); err == nil || err.Error() != "mic: no microphone" {
		t.Errorf("mute without a mic: %v", err)
	}

	fake.set(false, nil) // a mic is plugged in
	waitFor(t, "ready", is("mic.ready", true))
	waitFor(t, "on", func() bool { return l.statusText() == "on" })

	if err := l.do(deck.MicMute, nil, false); err != nil {
		t.Fatal(err)
	}
	if !is("mic.mute", true)() || l.statusText() != "muted" {
		t.Errorf("not muted at once: %v %q", state, l.statusText())
	}
	if err := l.do(deck.MicMute, nil, true); err != nil { // a release does nothing
		t.Fatal(err)
	}

	fake.set(false, nil) // unmuted by another app
	waitFor(t, "unmuted", is("mic.mute", false))

	// push to talk: unmuted while held, muted after, whatever it was before
	for _, release := range []bool{false, true} {
		if err := l.do(deck.MicPushToTalk, nil, release); err != nil {
			t.Fatal(err)
		}
	}
	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if len(calls) != 3 || !calls[0] || calls[1] || !calls[2] {
		t.Errorf("setMuted calls %v", calls)
	}

	if err := l.do("deafen", nil, false); err == nil {
		t.Error("an unknown op worked")
	}

	fake.set(false, errors.New("server gone"))
	waitFor(t, "not available", func() bool { return l.statusText() == "not available" })
	if !is("mic.ready", false)() {
		t.Errorf("still ready: %v", state)
	}
}

func TestMicUnsupported(t *testing.T) {
	l := newMicLink(func() (micBackend, error) { return nil, errors.ErrUnsupported }, func(map[string]bool) {})
	done := make(chan struct{})
	go func() {
		l.run(make(chan struct{}))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run kept going without a backend")
	}
}

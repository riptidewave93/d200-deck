package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/internal/alive"
)

// TestHelperProcess is the child the tests run: it appends a line to
// DECKD_TEST_LOG, then exits or, with DECKD_TEST_STAY, waits to be stopped;
// with DECKD_TEST_BEAT, it beats while it waits.
func TestHelperProcess(t *testing.T) {
	path := os.Getenv("DECKD_TEST_LOG")
	if path == "" {
		return
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	f.WriteString("start\n")
	f.Close()
	if os.Getenv("DECKD_TEST_BEAT") != "" {
		for range time.Tick(10 * time.Millisecond) {
			alive.Beat()
		}
	}
	if os.Getenv("DECKD_TEST_STAY") != "" {
		time.Sleep(time.Minute)
	}
	os.Exit(1)
}

func helper(t *testing.T, stay bool, env ...string) (func() *exec.Cmd, func() int) {
	path := filepath.Join(t.TempDir(), "log")
	return func() *exec.Cmd {
			c := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
			c.Env = append(os.Environ(), "DECKD_TEST_LOG="+path)
			if stay {
				c.Env = append(c.Env, "DECKD_TEST_STAY=1")
			}
			c.Env = append(c.Env, env...)
			return c
		}, func() int {
			b, _ := os.ReadFile(path)
			return strings.Count(string(b), "start")
		}
}

func fastRestarts(t *testing.T) {
	old := []time.Duration{firstDelay, maxDelay, stable, stopWait}
	firstDelay, maxDelay, stable, stopWait = time.Millisecond, 4*time.Millisecond, time.Hour, time.Second
	t.Cleanup(func() { firstDelay, maxDelay, stable, stopWait = old[0], old[1], old[2], old[3] })
}

func TestKeepRestarts(t *testing.T) {
	fastRestarts(t)
	cmd, starts := helper(t, false)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() { keep("crasher", cmd, quit, new(atomic.Int64)); close(done) }()
	for deadline := time.Now().Add(10 * time.Second); starts() < 3; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d starts", starts())
		}
	}
	close(quit)
	<-done
}

func TestKeepStops(t *testing.T) {
	fastRestarts(t)
	cmd, starts := helper(t, true)
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() { keep("stayer", cmd, quit, new(atomic.Int64)); close(done) }()
	for deadline := time.Now().Add(10 * time.Second); starts() < 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("never started")
		}
	}
	close(quit)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("not stopped")
	}
	if n := starts(); n != 1 {
		t.Errorf("%d starts", n)
	}
}

func TestKeepRestartsHung(t *testing.T) {
	fastRestarts(t)
	old := []time.Duration{beatEvery, hungAfter}
	beatEvery, hungAfter = 20*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { beatEvery, hungAfter = old[0], old[1] })
	for _, tc := range []struct {
		name  string
		env   []string
		hangs bool
	}{{"hung", nil, true}, {"beating", []string{"DECKD_TEST_BEAT=1"}, false}} {
		cmd, starts := helper(t, true, tc.env...)
		quit := make(chan struct{})
		done := make(chan struct{})
		var seen atomic.Int64
		go func() { keep(tc.name, cmd, quit, &seen); close(done) }()
		time.Sleep(time.Second)
		if n := starts(); (n > 1) != tc.hangs {
			t.Errorf("%s: %d starts", tc.name, n)
		}
		if since(&seen) > hungAfter {
			t.Errorf("%s: keeper not seen", tc.name)
		}
		close(quit)
		<-done
	}
}

func TestStuckPart(t *testing.T) {
	seen := make([]atomic.Int64, 2)
	now(&seen[0])
	seen[1].Store(-int64(2 * hungAfter)) // last seen before deckd started
	if got := stuckPart([]string{"keys", "update"}, seen); got != "update" {
		t.Errorf("stuck: %q", got)
	}
	now(&seen[1])
	if got := stuckPart([]string{"keys", "update"}, seen); got != "" {
		t.Errorf("stuck: %q", got)
	}
}

func TestPrograms(t *testing.T) {
	for _, name := range []string{"keys", "update"} {
		if programs[name] == nil {
			t.Errorf("no %s", name)
		}
	}
}

// Beats are timed on the monotonic clock, as time since deckd started, so
// setting the wall clock doesn't make a part look hung.
func TestBeatClock(t *testing.T) {
	var beat atomic.Int64
	now(&beat)
	if d := time.Duration(beat.Load()); d < 0 || d > time.Hour {
		t.Errorf("beat stored as %v: not time since start", d)
	}
	if s := since(&beat); s < 0 || s > time.Second {
		t.Errorf("since a beat just now: %v", s)
	}
}

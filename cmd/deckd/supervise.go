package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/riptidewave93/d200-deck/internal/alive"
)

// Restart timing: after firstDelay, doubling up to maxDelay while a program
// keeps exiting within stable of starting.
var (
	firstDelay = time.Second
	maxDelay   = 30 * time.Second
	stable     = 10 * time.Second
	stopWait   = 5 * time.Second // for a program to exit when stopped
)

// Hang checks: a part that doesn't beat for hungAfter is restarted. deckd
// writes a new count to aliveFile every beatEvery while it can keep its parts
// going, for init to restart it if that stops.
var (
	beatEvery = alive.Period
	hungAfter = 30 * time.Second
	aliveFile = "/run/deckd.alive"
)

var (
	errHung    = errors.New("hung")
	errStopped = errors.New("stopped")
)

// supervise runs each part until deckd is told to stop.
func supervise(names []string) {
	debug.SetMemoryLimit(4 << 20)
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	quit := make(chan struct{})
	var wg sync.WaitGroup
	seen := make([]atomic.Int64, len(names))
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keep(name, func() *exec.Cmd { return exec.Command(exe, name) }, quit, &seen[i])
		}()
	}
	tick := time.NewTicker(beatEvery)
	for n := 1; ; n++ {
		select {
		case s := <-stop:
			log.Printf("%v: stopping", s)
			close(quit)
			wg.Wait()
			return
		case <-tick.C:
			if stuck := stuckPart(names, seen); stuck != "" {
				log.Printf("%s won't stop: no heartbeat to init", stuck)
				continue
			}
			os.WriteFile(aliveFile, fmt.Appendf(nil, "%d\n", n), 0o644)
		}
	}
}

// stuckPart is a part whose keeper hasn't run for hungAfter, if any.
func stuckPart(names []string, seen []atomic.Int64) string {
	for i, name := range names {
		if since(&seen[i]) > hungAfter {
			return name
		}
	}
	return ""
}

func since(t *atomic.Int64) time.Duration { return time.Duration(time.Now().UnixNano() - t.Load()) }

func now(t *atomic.Int64) { t.Store(time.Now().UnixNano()) }

// keep runs the command cmd makes, again whenever it exits or hangs, until
// quit is closed; then it stops it. It notes in seen each time it runs.
func keep(name string, cmd func() *exec.Cmd, quit <-chan struct{}, seen *atomic.Int64) {
	tick := time.NewTicker(beatEvery)
	defer tick.Stop()
	delay := firstDelay
	for {
		now(seen)
		started := time.Now()
		err := run(name, cmd(), quit, tick.C, seen)
		if err == errStopped {
			return
		}
		if time.Since(started) >= stable {
			delay = firstDelay
		}
		log.Printf("%s exited (%v), restarting in %v", name, err, delay)
		wait := time.NewTimer(delay)
	sleep:
		for {
			select {
			case <-quit:
				return
			case <-wait.C:
				break sleep
			case <-tick.C:
				now(seen)
			}
		}
		delay = min(delay*2, maxDelay)
	}
}

// run runs c until it exits, hangs (errHung) or quit is closed (errStopped).
func run(name string, c *exec.Cmd, quit <-chan struct{}, tick <-chan time.Time, seen *atomic.Int64) error {
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	// if deckd dies, so does the part, and init restarts both
	c.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	if c.Env == nil {
		c.Env = os.Environ()
	}
	c.Env = append(c.Env, alive.Env+"=3")
	c.ExtraFiles = []*os.File{w}
	err = c.Start()
	w.Close()
	if err != nil {
		return err
	}
	var beat atomic.Int64
	now(&beat)
	go func() {
		b := make([]byte, 64)
		for {
			if _, err := r.Read(b); err != nil {
				return
			}
			now(&beat)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	for {
		select {
		case err := <-done:
			return err
		case <-tick:
			now(seen)
			if since(&beat) > hungAfter {
				log.Printf("%s: no heartbeat for %v: restarting it", name, hungAfter)
				stopCmd(c, done)
				return errHung
			}
		case <-quit:
			stopCmd(c, done)
			return errStopped
		}
	}
}

// stopCmd stops c: SIGTERM, then SIGKILL after stopWait.
func stopCmd(c *exec.Cmd, done <-chan error) {
	c.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(stopWait):
		c.Process.Kill()
		<-done
	}
}

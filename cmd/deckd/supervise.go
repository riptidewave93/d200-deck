package main

import (
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
)

// Restart timing: after firstDelay, doubling up to maxDelay while a program
// keeps exiting within stable of starting.
var (
	firstDelay = time.Second
	maxDelay   = 30 * time.Second
	stable     = 10 * time.Second
	stopWait   = 5 * time.Second // for a program to exit when stopped
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
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keep(name, func() *exec.Cmd { return exec.Command(exe, name) }, quit)
		}()
	}
	s := <-stop
	log.Printf("%v: stopping", s)
	close(quit)
	wg.Wait()
}

// keep runs the command cmd makes, again whenever it exits, until quit is
// closed; then it stops it.
func keep(name string, cmd func() *exec.Cmd, quit <-chan struct{}) {
	delay := firstDelay
	for {
		c := cmd()
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		// if deckd dies, so does the part, and init restarts both
		c.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
		started := time.Now()
		err := c.Start()
		if err == nil {
			done := make(chan error, 1)
			go func() { done <- c.Wait() }()
			select {
			case err = <-done:
			case <-quit:
				c.Process.Signal(syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(stopWait):
					c.Process.Kill()
					<-done
				}
				return
			}
		}
		if time.Since(started) >= stable {
			delay = firstDelay
		}
		log.Printf("%s exited (%v), restarting in %v", name, err, delay)
		select {
		case <-quit:
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, maxDelay)
	}
}

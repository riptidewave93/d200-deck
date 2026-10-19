package main

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// run carries out a PC action.
func run(a deck.Action) error {
	switch a.Type {
	case deck.ActionOpenURL:
		return openURL(a.URL)
	case deck.ActionLaunch:
		return start(a.Command...)
	case deck.ActionHTTP:
		return sendHTTP(a)
	}
	return fmt.Errorf("deckhost can't run %q actions", a.Type)
}

// openURL opens u in the default browser.
var openURL = func(u string) error { return start("xdg-open", u) }

// start runs a program in its own session, so it outlives the daemon.
func start(argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func sendHTTP(a deck.Action) error {
	method := a.Method
	if method == "" {
		method = http.MethodGet
		if a.Body != "" {
			method = http.MethodPost
		}
	}
	req, err := http.NewRequest(method, a.URL, strings.NewReader(a.Body))
	if err != nil {
		return err
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %s", method, a.URL, resp.Status)
	}
	return nil
}

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// run carries out a PC action. It checks URLs again, so a device that
// isn't ours can't open files or other programs through them.
func run(a deck.Action) error {
	if (a.Type == deck.ActionOpenURL || a.Type == deck.ActionHTTP) && !webURL(a.URL) {
		return errors.New("not an http(s) URL")
	}
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

func webURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.Opaque == ""
}

// start runs a program detached from deckhost, so it outlives the daemon.
func start(argv ...string) error {
	if len(argv) == 0 || argv[0] == "" {
		return errors.New("no command to run")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	cmd := detached(path, argv[1:])
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
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // without the URL, which can be a secret
		}
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: %s", method, resp.Status)
	}
	return nil
}

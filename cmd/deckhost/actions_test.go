package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/riptidewave93/d200-deck/deck"
)

// fakeBrowser records the URLs openURL is asked to open.
func fakeBrowser(t *testing.T) func() []string {
	var mu sync.Mutex
	var urls []string
	old := openURL
	openURL = func(u string) error {
		mu.Lock()
		defer mu.Unlock()
		urls = append(urls, u)
		return nil
	}
	t.Cleanup(func() { openURL = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), urls...)
	}
}

func TestRunHTTP(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+r.Header.Get("X-Test")+" "+string(body))
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	if err := run(deck.Action{Type: deck.ActionHTTP, URL: srv.URL + "/get"}); err != nil {
		t.Error(err)
	}
	if err := run(deck.Action{Type: deck.ActionHTTP, URL: srv.URL + "/post", Body: "hi", Headers: map[string]string{"X-Test": "1"}}); err != nil {
		t.Error(err)
	}
	if err := run(deck.Action{Type: deck.ActionHTTP, Method: "PUT", URL: srv.URL + "/put"}); err != nil {
		t.Error(err)
	}
	if err := run(deck.Action{Type: deck.ActionHTTP, URL: srv.URL + "/fail"}); err == nil {
		t.Error("no error for a 500")
	}
	want := []string{"GET /get  ", "POST /post 1 hi", "PUT /put  ", "GET /fail  "}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests %q", got)
	}
	if run(deck.Action{Type: deck.ActionHTTP, Method: "BAD METHOD", URL: srv.URL}) == nil {
		t.Error("no error for a bad method")
	}
	if run(deck.Action{Type: deck.ActionHTTP, URL: "http://127.0.0.1:1/"}) == nil {
		t.Error("no error for a refused connection")
	}
}

func TestRunOther(t *testing.T) {
	urls := fakeBrowser(t)
	if err := run(deck.Action{Type: deck.ActionOpenURL, URL: "https://example.com"}); err != nil {
		t.Error(err)
	}
	if u := urls(); len(u) != 1 || u[0] != "https://example.com" {
		t.Errorf("opened %v", u)
	}
	if err := run(deck.Action{Type: deck.ActionLaunch, Command: []string{"true"}}); err != nil {
		t.Error(err)
	}
	if run(deck.Action{Type: deck.ActionLaunch, Command: []string{"/no/such/program"}}) == nil {
		t.Error("launched a missing program")
	}
	for _, cmd := range [][]string{nil, {""}} {
		if run(deck.Action{Type: deck.ActionLaunch, Command: cmd}) == nil {
			t.Errorf("launched %q", cmd)
		}
	}
	if run(deck.Action{Type: deck.ActionKey}) == nil {
		t.Error("ran a device action")
	}
}

func TestWebURL(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://example.com/x": true, "http://localhost:8080": true,
		"file:///etc/passwd": false, `\\host\share\x.exe`: false, `C:\Windows\System32\cmd.exe`: false,
		"javascript:alert(1)": false, "https:example.com": false, "": false,
	} {
		if webURL(u) != ok {
			t.Errorf("%q: %v", u, !ok)
		}
	}
	if err := run(deck.Action{Type: deck.ActionOpenURL, URL: "file:///etc/passwd"}); err == nil {
		t.Error("opened a file URL")
	}
}

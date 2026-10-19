package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptidewave93/d200-deck/internal/serial"
)

func get(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func server(t *testing.T, content string) *httptest.Server {
	p := filepath.Join(t.TempDir(), "os-release")
	if content != "" {
		os.WriteFile(p, []byte(content), 0o644)
	}
	old := osRelease
	osRelease = p
	t.Cleanup(func() { osRelease = old })
	srv := httptest.NewServer(handler("d200-00112233445566aa.local"))
	t.Cleanup(srv.Close)
	return srv
}

func TestUpdatePage(t *testing.T) {
	srv := server(t, "VERSION_ID=v1.2.3\nVARIANT_ID=release\nBUILD_ID=<b>20261019\n")
	req, _ := http.NewRequest("GET", srv.URL+"/update", nil)
	resp, body := get(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"<dd>v1.2.3</dd>", "<dd>release</dd>", "<dd>&lt;b&gt;20261019</dd>"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(body, "{version}") {
		t.Error("placeholder left in the page")
	}
}

func TestUnknownVersion(t *testing.T) {
	srv := server(t, "")
	req, _ := http.NewRequest("GET", srv.URL+"/update", nil)
	if _, body := get(t, req); strings.Count(body, "<dd>unknown</dd>") != 3 {
		t.Error("missing os-release not shown as unknown")
	}
}

func TestRoutes(t *testing.T) {
	srv := server(t, "VERSION_ID=1\n")
	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	if resp, _ := get(t, req); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/update" {
		t.Errorf("/: %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	req, _ = http.NewRequest("POST", srv.URL+"/update", strings.NewReader("firmware"))
	if resp, _ := get(t, req); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /update: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/other", nil)
	if resp, _ := get(t, req); resp.StatusCode != http.StatusNotFound {
		t.Errorf("/other: %d", resp.StatusCode)
	}
}

func TestOtherHostRefused(t *testing.T) {
	srv := server(t, "VERSION_ID=1\n")
	req, _ := http.NewRequest("GET", srv.URL+"/update", nil)
	req.Host = "attacker.example"
	if resp, _ := get(t, req); resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("rebound host: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/update", nil)
	req.Host = "127.0.0.1" // the IP without a port is fine
	if resp, _ := get(t, req); resp.StatusCode != http.StatusOK {
		t.Errorf("IP without port: %d", resp.StatusCode)
	}
	for _, host := range []string{"d200-00112233445566aa.local", "D200-00112233445566AA.local.:80"} {
		req, _ = http.NewRequest("GET", srv.URL+"/update", nil)
		req.Host = host
		if resp, _ := get(t, req); resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", host, resp.StatusCode)
		}
	}
	req, _ = http.NewRequest("GET", srv.URL+"/update", nil)
	req.Host = "d200-ffeeddccbbaa9988.local" // another deck's name
	if resp, _ := get(t, req); resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("other deck's name: %d", resp.StatusCode)
	}
}

func TestMDNSName(t *testing.T) {
	old := serial.Path
	t.Cleanup(func() { serial.Path = old })
	serial.Path = filepath.Join(t.TempDir(), "serial-number")
	if n := mdnsName(); n != "" {
		t.Errorf("no serial: %q", n)
	}
	os.WriteFile(serial.Path, []byte("00112233445566AA\x00"), 0o644)
	if n := mdnsName(); n != "d200-00112233445566aa.local" {
		t.Errorf("name %q", n)
	}
	os.WriteFile(serial.Path, []byte("not hex\x00"), 0o644)
	if n := mdnsName(); n != "" {
		t.Errorf("bad serial: %q", n)
	}
}

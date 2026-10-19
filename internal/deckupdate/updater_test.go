package deckupdate

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/ota"
)

type fakeEnv struct {
	mu   sync.Mutex
	vars map[string]string
}

func (e *fakeEnv) Get() (map[string]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := map[string]string{}
	for k, v := range e.vars {
		c[k] = v
	}
	return c, nil
}

func (e *fakeEnv) Set(kv ...string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := 0; i+1 < len(kv); i += 2 {
		e.vars[kv[i]] = kv[i+1]
	}
	return nil
}

type fakeScreen struct {
	answer bool
	err    error
	mu     sync.Mutex
	steps  []string
	closed bool
}

func (s *fakeScreen) Confirm(ota.ConfirmRequest) (bool, error) { return s.answer, s.err }

func (s *fakeScreen) Progress(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

func (s *fakeScreen) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeScreen) done() ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...), s.closed
}

type fakes struct {
	env        *fakeEnv
	screen     *fakeScreen
	installErr error
	installed  []string // versions
	reboots    int
	mu         sync.Mutex
}

func testUpdater(t *testing.T, flavour string) (*updater, *fakes) {
	oldDelay := rebootDelay
	rebootDelay = 0
	t.Cleanup(func() { rebootDelay = oldDelay })
	dir := t.TempDir()
	f := &fakes{env: &fakeEnv{vars: map[string]string{"slot": "a"}}, screen: &fakeScreen{answer: true}}
	u := &updater{
		dir: filepath.Join(dir, "update"), statePath: filepath.Join(dir, "state.json"),
		installed: osrelease.Info{Version: "old", Flavour: flavour, Build: "202610190000"},
		slot:      "a",
		env:       f.env,
		screen: func() (screen, error) {
			if f.screen == nil {
				return nil, errors.New("no deckd")
			}
			return f.screen, nil
		},
		reboot:    func() error { f.mu.Lock(); f.reboots++; f.mu.Unlock(); return nil },
		freeSpace: func(string) (int64, error) { return 100 << 20, nil },
		healthy:   func() bool { return true },
		install: func(m *ota.Manifest, progress func(string, int, int)) ([]ota.Result, error) {
			progress("kernel", 3, 4)
			f.mu.Lock()
			f.installed = append(f.installed, m.Version)
			f.mu.Unlock()
			return []ota.Result{{Target: "kernel", Written: true}}, f.installErr
		},
	}
	os.MkdirAll(u.dir, 0o755)
	u.load()
	u.start()
	waitState(t, u, stateIdle)
	return u, f
}

func waitState(t *testing.T, u *updater, want string) Status {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if s := u.status(); s.State == want {
			return s
		}
	}
	t.Fatalf("state %+v, want %s", u.status(), want)
	return Status{}
}

// bundle is an update bundle (with fake images) for version.
func bundle(t *testing.T, version, flavour string, build int64) []byte {
	m := ota.Manifest{Format: 1, Product: "d200", Flavour: flavour, Version: version, BuildID: build}
	files := map[string][]byte{"idbloader": bytes.Repeat([]byte{0xff}, 256<<10), "u-boot": []byte("u"), "kernel": []byte("k"), "rootfs": []byte("r")}
	for _, target := range ota.Targets {
		sum := sha256.Sum256(files[target])
		m.Images = append(m.Images, ota.Image{Target: target, File: target + ".img", Size: int64(len(files[target])), SHA256: hex.EncodeToString(sum[:])})
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, b []byte) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg})
		tw.Write(b)
	}
	mb, _ := json.Marshal(m)
	add(ota.ManifestFile, mb)
	for _, target := range ota.Targets {
		add(target+".img", files[target])
	}
	tw.Close()
	return buf.Bytes()
}

func put(t *testing.T, u *updater, body []byte, header bool) (int, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(u.upload))
	defer srv.Close()
	req, _ := http.NewRequest("PUT", srv.URL, bytes.NewReader(body))
	if header {
		req.Header.Set("X-D200-Update", "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b bytes.Buffer
	b.ReadFrom(resp.Body)
	return resp.StatusCode, b.String()
}

func TestInstallFlow(t *testing.T) {
	u, f := testUpdater(t, "debug")
	if code, _ := put(t, u, bundle(t, "new", "debug", 202610200000), false); code != http.StatusForbidden {
		t.Errorf("no header: %d", code)
	}
	if code, body := put(t, u, bundle(t, "new", "debug", 202610200000), true); code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", code, body)
	}
	s := waitState(t, u, stateRebooting)
	var steps []string
	for closed := false; !closed; time.Sleep(5 * time.Millisecond) { // run ends after the reboot
		steps, closed = f.screen.done()
	}
	f.mu.Lock()
	reboots, installed := f.reboots, f.installed
	f.mu.Unlock()
	if s.Update.Version != "new" || installed[0] != "new" || reboots != 1 {
		t.Errorf("status %+v, installed %q, %d reboots", s, installed, reboots)
	}
	if len(steps) != 2 || steps[0] != "kernel (3/4)" || steps[1] != "Restarting" {
		t.Errorf("screen steps %q", steps)
	}
	if s.Slots["b"].Version != "new" || s.Slots["a"].Version != "old" || s.Last == nil || s.Last.Slot != "b" {
		t.Errorf("slots %+v, last %+v", s.Slots, s.Last)
	}
	// saved for after the reboot
	var sv saved
	b, _ := os.ReadFile(u.statePath)
	if json.Unmarshal(b, &sv); sv.Slots["b"].Version != "new" || sv.Last.Version != "new" {
		t.Errorf("saved %s", b)
	}
	if code, _ := put(t, u, bundle(t, "newer", "debug", 202610210000), true); code != http.StatusConflict {
		t.Errorf("upload while rebooting: %d", code)
	}
}

func TestInstallCancelledOrFailed(t *testing.T) {
	u, f := testUpdater(t, "debug")
	f.screen.answer = false
	put(t, u, bundle(t, "new", "debug", 1), true)
	waitState(t, u, stateCancelled)
	if len(f.installed) != 0 {
		t.Error("installed after Cancel")
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if e, _ := os.ReadDir(u.dir); len(e) == 0 {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("%d files left after Cancel", len(e))
		}
	}

	f.screen = &fakeScreen{answer: true}
	f.installErr = errors.New("kernel: I/O error")
	old := failDelay
	failDelay = 0
	t.Cleanup(func() { failDelay = old })
	put(t, u, bundle(t, "new", "debug", 1), true)
	s := waitState(t, u, stateFailed)
	if s.Error != "kernel: I/O error" || s.Last.Error == "" || s.Slots["b"].Version != "" || f.reboots != 0 {
		t.Errorf("%+v", s)
	}

	f.screen = nil // deckd isn't there
	put(t, u, bundle(t, "new", "debug", 1), true)
	if s := waitState(t, u, stateFailed); s.Error == "" {
		t.Error("no error without deckd")
	}
}

func TestUploadRefused(t *testing.T) {
	u, _ := testUpdater(t, "release")
	if code, body := put(t, u, bundle(t, "new", "debug", 1), true); code != http.StatusBadRequest {
		t.Errorf("debug on release: %d %s", code, body)
	}
	if code, _ := put(t, u, []byte("not a tar"), true); code != http.StatusBadRequest {
		t.Errorf("garbage: %d", code)
	}
	u.freeSpace = func(string) (int64, error) { return 1 << 20, nil }
	if code, _ := put(t, u, bundle(t, "new", "release", 1), true); code != http.StatusInsufficientStorage {
		t.Errorf("no room: %d", code)
	}
	u.set(stateChecking, "", nil)
	if code, _ := put(t, u, bundle(t, "new", "release", 1), true); code != http.StatusConflict {
		t.Errorf("while checking: %d", code)
	}
	u.set(stateUnsupported, "", nil)
	if code, _ := put(t, u, bundle(t, "new", "release", 1), true); code != http.StatusConflict {
		t.Errorf("unsupported: %d", code)
	}
}

func TestStartConfirmsTrial(t *testing.T) {
	dir := t.TempDir()
	env := &fakeEnv{vars: map[string]string{"slot": "b", "upgrade_available": "1", "ota_result": "trial"}}
	u := &updater{dir: dir, statePath: filepath.Join(dir, "state.json"), slot: "b", env: env,
		installed: osrelease.Info{Version: "new"}, healthy: func() bool { return true }}
	u.start()
	s := waitState(t, u, stateIdle)
	if env.vars["ota_result"] != "ok" || s.OTAResult != "ok" || s.Slots["b"].Version != "new" {
		t.Errorf("%+v %v", s, env.vars)
	}
}

func TestDeckdHealthy(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	if deckdHealthy(ready, 10*time.Millisecond, 0) {
		t.Error("healthy without a ready file")
	}
	os.WriteFile(ready, []byte(strconv.Itoa(os.Getpid())), 0o644)
	if !deckdHealthy(ready, time.Second, time.Millisecond) {
		t.Error("not healthy with a running PID")
	}
	os.WriteFile(ready, []byte("999999999"), 0o644)
	if deckdHealthy(ready, 10*time.Millisecond, 0) {
		t.Error("healthy with a dead PID")
	}
}

func TestDeckdScreen(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := make(chan ota.UIRequest, 4)
	go func() {
		c, _ := l.Accept()
		defer c.Close()
		dec := json.NewDecoder(c)
		for {
			var r ota.UIRequest
			if dec.Decode(&r) != nil {
				return
			}
			got <- r
			if r.Confirm != nil {
				json.NewEncoder(c).Encode(ota.UIReply{OK: true})
			}
		}
	}()
	sc, err := dialDeckd(sock)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := sc.Confirm(ota.ConfirmRequest{Version: "v"}); !ok || err != nil {
		t.Errorf("confirm %v %v", ok, err)
	}
	sc.Progress("step")
	if r := <-got; r.Confirm.Version != "v" {
		t.Errorf("%+v", r)
	}
	if r := <-got; r.Progress != "step" {
		t.Errorf("%+v", r)
	}
	sc.Close()
	if _, err := dialDeckd(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("dialled nothing")
	}
}

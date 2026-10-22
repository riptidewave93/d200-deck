package deckd

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/proto"
)

// fakeEnv is a U-Boot env as fw_setenv changes it: a name without a value
// removes it.
type fakeEnv struct {
	vars           map[string]string
	getErr, setErr error
	sets           int
}

func (e *fakeEnv) Get() (map[string]string, error) {
	if e.getErr != nil {
		return nil, e.getErr
	}
	return maps.Clone(e.vars), nil
}

func (e *fakeEnv) Set(kv ...string) error {
	if e.setErr != nil {
		return e.setErr
	}
	e.sets++
	for i := 0; i < len(kv); i += 2 {
		if i+1 < len(kv) {
			e.vars[kv[i]] = kv[i+1]
		} else {
			delete(e.vars, kv[i])
		}
	}
	return nil
}

// panelTest fakes the command line and env, and counts reboots.
func panelTest(t *testing.T, cmdline string, env *fakeEnv) (*deckd, chan struct{}) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cmdline")
	os.WriteFile(path, []byte(cmdline), 0o644)
	rebooted := make(chan struct{}, 4)
	oldPath, oldEnv, oldReboot, oldDelay := cmdlinePath, panelEnv, reboot, resetDelay
	t.Cleanup(func() { cmdlinePath, panelEnv, reboot, resetDelay = oldPath, oldEnv, oldReboot, oldDelay })
	cmdlinePath, panelEnv, resetDelay = path, env, 0
	reboot = func() error { rebooted <- struct{}{}; return nil }
	return newTestDeckd(), rebooted
}

func panelSettings(t *testing.T, d *deckd, payload string) proto.Settings {
	t.Helper()
	status, reply := d.settings(1, []byte(payload))
	var s proto.Settings
	if status != proto.StatusOK || json.Unmarshal(reply, &s) != nil {
		t.Fatalf("%q: status %d, reply %q", payload, status, reply)
	}
	return s
}

func str(p *string) string {
	if p == nil {
		return "<none>"
	}
	return *p
}

func TestPanel(t *testing.T) {
	env := &fakeEnv{vars: map[string]string{"slot": "a"}}
	d, rebooted := panelTest(t, "console=ttyS2 d200.slot=a d200.panel=h1 d200.panel_regs=00112233445566\n", env)
	restarted := func(want bool) {
		t.Helper()
		if !want {
			select {
			case <-rebooted:
				t.Error("rebooted")
			case <-time.After(50 * time.Millisecond):
			}
			return
		}
		runUI(t, d)
		if d.overlay == nil || d.overlay.keys[7].Label != "Restarting" {
			t.Error("restart not shown")
		}
		d.overlay = nil
		select {
		case <-rebooted:
		case <-time.After(time.Second):
			t.Error("not rebooted")
		}
	}

	if s := panelSettings(t, d, ""); str(s.Panel) != "auto" || str(s.PanelActive) != "h1" {
		t.Errorf("panel %s, active %s", str(s.Panel), str(s.PanelActive))
	}

	s := panelSettings(t, d, `{"panel": "h3"}`)
	if str(s.Panel) != "h3" || str(s.PanelActive) != "h1" || env.vars["panel"] != "h3" {
		t.Errorf("panel %s, active %s, env %q", str(s.Panel), str(s.PanelActive), env.vars["panel"])
	}
	restarted(true)

	panelSettings(t, d, `{"panel": "h3"}`) // no change
	panelSettings(t, d, `{"brightness": 50}`)
	restarted(false)

	if s := panelSettings(t, d, `{"panel": "auto"}`); str(s.Panel) != "auto" {
		t.Errorf("panel %s", str(s.Panel))
	}
	if _, ok := env.vars["panel"]; ok || env.vars["slot"] != "a" {
		t.Errorf("env %v", env.vars)
	}
	restarted(true)

	if status, reply := d.settings(1, []byte(`{"panel": "h2", "host_os": "mac"}`)); status != proto.StatusFailed || string(reply) != `panel must be "auto", "h1" or "h3"` {
		t.Errorf("status %d, reply %q", status, reply)
	}
	if d.host.get() != "" {
		t.Error("a failed change changed host_os")
	}
	if env.sets != 2 {
		t.Errorf("%d env writes", env.sets)
	}
}

// An override U-Boot ignores reads as auto.
func TestPanelUnknownOverride(t *testing.T) {
	d, _ := panelTest(t, "d200.panel=h3", &fakeEnv{vars: map[string]string{"panel": "h9"}})
	if s := panelSettings(t, d, ""); str(s.Panel) != "auto" || str(s.PanelActive) != "h3" {
		t.Errorf("panel %s, active %s", str(s.Panel), str(s.PanelActive))
	}
}

// Firmware that can't pick the panel, or an env that can't be read, leaves
// the panel out, and a change does nothing.
func TestPanelUnsupported(t *testing.T) {
	for name, c := range map[string]struct {
		cmdline string
		env     *fakeEnv
	}{
		"old firmware": {"console=ttyS2 d200.slot=a", &fakeEnv{vars: map[string]string{}}},
		"no env":       {"d200.panel=h1", &fakeEnv{getErr: errors.New("fw_printenv: exit status 1")}},
	} {
		t.Run(name, func(t *testing.T) {
			d, rebooted := panelTest(t, c.cmdline, c.env)
			for _, payload := range []string{"", `{"panel": "h3"}`} {
				if s := panelSettings(t, d, payload); s.Panel != nil || s.PanelActive != nil || s.USBDrive == nil {
					t.Errorf("%q: panel %s, active %s", payload, str(s.Panel), str(s.PanelActive))
				}
			}
			if c.env.sets != 0 || len(rebooted) != 0 {
				t.Error("changed")
			}
		})
	}
}

// A failed write fails the change, without a restart.
func TestPanelWriteFails(t *testing.T) {
	env := &fakeEnv{vars: map[string]string{}, setErr: errors.New("fw_setenv: exit status 1")}
	d, rebooted := panelTest(t, "d200.panel=h1", env)
	if status, reply := d.settings(1, []byte(`{"panel": "h1"}`)); status != proto.StatusFailed || string(reply) != "fw_setenv: exit status 1" {
		t.Errorf("status %d, reply %q", status, reply)
	}
	if s := panelSettings(t, d, ""); str(s.Panel) != "auto" {
		t.Errorf("panel %s", str(s.Panel))
	}
	if len(rebooted) != 0 || len(d.ui) != 0 {
		t.Error("restarted")
	}
}

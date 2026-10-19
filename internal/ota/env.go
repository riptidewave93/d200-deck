package ota

import (
	"fmt"
	"os/exec"
	"strings"
)

// Env is the U-Boot environment.
type Env interface {
	Get() (map[string]string, error)
	Set(kv ...string) error // name, value, ...: one write
}

// FWEnv is the environment through fw_printenv and fw_setenv.
type FWEnv struct{}

func (FWEnv) Get() (map[string]string, error) {
	out, err := exec.Command("fw_printenv").Output()
	if err != nil {
		return nil, fmt.Errorf("fw_printenv: %w", err)
	}
	env := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			env[k] = v
		}
	}
	return env, nil
}

func (FWEnv) Set(kv ...string) error {
	if out, err := exec.Command("fw_setenv", kv...).CombinedOutput(); err != nil {
		return fmt.Errorf("fw_setenv: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RunningSlot is the slot the kernel command line says booted, "" if none.
func RunningSlot(cmdline string) string {
	for _, f := range strings.Fields(cmdline) {
		if s, ok := strings.CutPrefix(f, "d200.slot="); ok && (s == "a" || s == "b") {
			return s
		}
	}
	return ""
}

// Other is the other slot.
func Other(slot string) string {
	if slot == "a" {
		return "b"
	}
	return "a"
}

// Boot outcomes, as Confirm reports them.
const (
	Confirmed = "ok"
	Failed    = "failed"
	Rebooting = "rebooting"
)

// Confirm settles a trial boot at startup: if the new slot is running and
// healthy it's kept; if U-Boot couldn't boot it, the env goes back to the
// running slot. It returns what it did, "" if there was no trial.
func Confirm(env Env, running string, healthy func() bool, reboot func() error) (string, error) {
	e, err := env.Get()
	if err != nil || running == "" || e["upgrade_available"] != "1" {
		return "", err
	}
	if e["slot"] != running {
		return Failed, env.Set("slot", running, "upgrade_available", "0", "bootcount", "0", "ota_result", Failed)
	}
	if healthy() {
		return Confirmed, env.Set("upgrade_available", "0", "bootcount", "0", "ota_result", Confirmed)
	}
	return Rebooting, reboot() // counts towards U-Boot's rollback
}

package ota

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type fakeEnv struct {
	vars map[string]string
	sets [][]string
	fail bool
}

func (e *fakeEnv) Get() (map[string]string, error) {
	if e.fail {
		return nil, errIO
	}
	return e.vars, nil
}

func (e *fakeEnv) Set(kv ...string) error {
	e.sets = append(e.sets, kv)
	for i := 0; i+1 < len(kv); i += 2 {
		e.vars[kv[i]] = kv[i+1]
	}
	return nil
}

type closingNAND struct{ *fakeNAND }

func (closingNAND) Close() error { return nil }

func TestInstall(t *testing.T) {
	m, ms := testBundle()
	dir := t.TempDir()
	if _, err := Receive(bytes.NewReader(tarOf(t, m, ms)), dir); err != nil {
		t.Fatal(err)
	}
	ub := ubootImage()[:1000]
	os.WriteFile(filepath.Join(dir, "idbloader.img"), idbImage(), 0o644) // the test bundle's isn't a real IDB
	os.WriteFile(filepath.Join(dir, "u-boot.itb"), ub, 0o644)
	parts := map[string]*fakeNAND{"idbloader": newNAND(1 << 20), "u-boot": newNAND(4 << 20), "u-boot-backup": newNAND(4 << 20)}
	env := &fakeEnv{vars: map[string]string{"slot": "a"}}
	u := &fakeUBI{vols: map[int]*fakeVol{}}
	var steps []string
	in := &Installer{
		Dir: dir,
		OpenNAND: func(name string) (NANDCloser, error) {
			if p, ok := parts[name]; ok {
				return closingNAND{p}, nil
			}
			return nil, errors.New("no " + name)
		},
		UBI:      u,
		Env:      env,
		Progress: func(s string, n, of int) { steps = append(steps, s) },
	}
	// the manifest's sizes and hashes are for the test files: fix the two replaced
	m.Images[1] = imageOf("u-boot", "u-boot.itb", ub)
	results, err := in.Install(m, "a")
	if err != nil {
		t.Fatalf("%v %+v", err, results)
	}
	if len(results) != 4 || results[3].Name != "System" || len(steps) != 4 || string(u.vols[3].data) != "kernel" || string(u.vols[5].data) != "rootfs" {
		t.Errorf("results %+v, steps %q", results, steps)
	}
	want := []string{"slot", "b", "upgrade_available", "1", "bootcount", "0", "ota_result", "trial"}
	if len(env.sets) != 1 || !slices.Equal(env.sets[0], want) {
		t.Errorf("env sets %q", env.sets)
	}

	// again: the same kernel isn't written again, and a failure (the
	// system's) stops it, and the slot isn't switched
	env.sets = nil
	u.failUpdate = 2
	results, err = in.Install(m, "a")
	if err == nil || len(results) != 4 || !results[2].Skipped || results[3].Error == "" || len(env.sets) != 0 {
		t.Errorf("%v %+v %q", err, results, env.sets)
	}
	delete(parts, "u-boot")
	if _, err := in.Install(m, "a"); err == nil {
		t.Error("no u-boot partition, but it worked")
	}
}

func imageOf(target, file string, data []byte) Image {
	_, im := imageFileData(data)
	im.Target, im.File = target, file
	return im
}

func TestConfirm(t *testing.T) {
	healthy := func(ok bool) func() bool { return func() bool { return ok } }
	rebooted := false
	reboot := func() error { rebooted = true; return nil }

	env := &fakeEnv{vars: map[string]string{"slot": "b", "upgrade_available": "1", "bootcount": "1", "ota_result": "trial"}}
	if got, err := Confirm(env, "b", healthy(true), reboot); got != Confirmed || err != nil || env.vars["ota_result"] != "ok" || env.vars["upgrade_available"] != "0" {
		t.Errorf("trial ok: %q %v %v", got, err, env.vars)
	}
	if got, _ := Confirm(env, "b", healthy(true), reboot); got != "" || len(env.sets) != 1 {
		t.Errorf("no trial: %q", got)
	}

	env = &fakeEnv{vars: map[string]string{"slot": "b", "upgrade_available": "1", "ota_result": "trial"}}
	if got, _ := Confirm(env, "b", healthy(false), reboot); got != Rebooting || !rebooted || len(env.sets) != 0 {
		t.Errorf("unhealthy: %q", got)
	}

	// U-Boot fell back to a in the same boot
	env = &fakeEnv{vars: map[string]string{"slot": "a", "upgrade_available": "1", "ota_result": "trial"}}
	if got, _ := Confirm(env, "b", healthy(true), reboot); got != Failed || env.vars["slot"] != "b" || env.vars["ota_result"] != "failed" {
		t.Errorf("fell back: %q %v", got, env.vars)
	}

	// no trial, but the stable slot didn't start
	env = &fakeEnv{vars: map[string]string{"slot": "a", "upgrade_available": "0", "ota_result": "ok"}}
	if got, _ := Confirm(env, "b", healthy(true), reboot); got != FellBack || env.vars["slot"] != "b" || env.vars["ota_result"] != FellBack {
		t.Errorf("fell back without a trial: %q %v", got, env.vars)
	}
	env = &fakeEnv{vars: map[string]string{}} // never updated
	if got, _ := Confirm(env, "a", healthy(true), reboot); got != "" || len(env.sets) != 0 {
		t.Errorf("fresh env: %q %v", got, env.vars)
	}

	if _, err := Confirm(&fakeEnv{fail: true}, "a", healthy(true), reboot); err == nil {
		t.Error("env error hidden")
	}
	if RunningSlot("console=ttyS2 d200.slot=b root=ubi0:rootfs_b ro") != "b" || RunningSlot("quiet") != "" || Other("a") != "b" || Other("b") != "a" {
		t.Error("slots")
	}
}

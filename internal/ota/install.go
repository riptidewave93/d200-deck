package ota

import (
	"fmt"
	"os"
	"path/filepath"
)

// NANDCloser is an open MTD partition.
type NANDCloser interface {
	NAND
	Close() error
}

// Installer writes a received bundle and activates it.
type Installer struct {
	Dir      string // the bundle's files
	OpenNAND func(name string) (NANDCloser, error)
	UBI      UBI
	Env      Env
	Progress func(step string, n, of int)
}

// Install writes the bundle in m to the bootloader and the slot that isn't
// running, then sets the env to try that slot at the next boot. On an error
// it stops, leaving what was written.
func (in *Installer) Install(m *Manifest, running string) ([]Result, error) {
	slot := Other(running)
	kernel, rootfs := SlotVolumes(slot)
	steps := []struct {
		name string
		run  func() Result
	}{
		{"IDB loader", func() Result { return in.idb(m) }},
		{"U-Boot", func() Result { return in.uboot(m) }},
		{"Kernel", func() Result {
			return WriteVolume(in.UBI, kernel, "kernel", in.path(m, "kernel"), m.Image("kernel"))
		}},
		{"System", func() Result {
			return WriteVolume(in.UBI, rootfs, "rootfs", in.path(m, "rootfs"), m.Image("rootfs"))
		}},
	}
	var results []Result
	for i, s := range steps {
		if in.Progress != nil {
			in.Progress(s.name, i+1, len(steps))
		}
		r := s.run()
		r.Name = s.name
		results = append(results, r)
		if r.Error != "" {
			return results, fmt.Errorf("%s: %s", s.name, r.Error)
		}
	}
	err := in.Env.Set("slot", slot, "upgrade_available", "1", "bootcount", "0", "ota_result", "trial")
	return results, err
}

func (in *Installer) path(m *Manifest, target string) string {
	return filepath.Join(in.Dir, m.Image(target).File)
}

func (in *Installer) read(m *Manifest, target string) ([]byte, error) {
	return os.ReadFile(in.path(m, target))
}

func (in *Installer) idb(m *Manifest) Result {
	img, err := in.read(m, "idbloader")
	if err != nil {
		return Result{Target: "idbloader", Error: err.Error()}
	}
	dev, err := in.OpenNAND("idbloader")
	if err != nil {
		return Result{Target: "idbloader", Error: err.Error()}
	}
	defer dev.Close()
	return WriteIDB(dev, img)
}

func (in *Installer) uboot(m *Manifest) Result {
	fail := func(err error) Result { return Result{Target: "u-boot", Error: err.Error()} }
	img, err := in.read(m, "u-boot")
	if err != nil {
		return fail(err)
	}
	backup, err := in.OpenNAND("u-boot-backup")
	if err != nil {
		return fail(err)
	}
	defer backup.Close()
	main, err := in.OpenNAND("u-boot")
	if err != nil {
		return fail(err)
	}
	defer main.Close()
	return WriteUBoot(backup, main, img)
}

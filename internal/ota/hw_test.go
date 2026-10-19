//go:build linux

package ota

import (
	"os"
	"path/filepath"
	"testing"
)

// The hardware tests run on a D200 (go test -c, then ./ota.test with
// D200_BUNDLE set to an update bundle). They only read the bootloader and
// write slot B, which must not be the running slot.
func hwBundle(t *testing.T) (*Manifest, string) {
	path := os.Getenv("D200_BUNDLE")
	if path == "" || RunningSlot(readFile("/proc/cmdline")) == "" {
		t.Skip("not on a D200 with D200_BUNDLE set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dir := t.TempDir()
	m, err := Receive(f, dir)
	if err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestHWBootloaderMatches(t *testing.T) {
	m, dir := hwBundle(t)
	idb, err := OpenMTD("idbloader")
	if err != nil {
		t.Fatal(err)
	}
	defer idb.Close()
	t.Logf("idbloader: %d bytes, blocks %d, pages %d+%d", idb.Size(), idb.EraseSize(), idb.PageSize(), idb.OOBSize())
	img, _ := os.ReadFile(filepath.Join(dir, m.Image("idbloader").File))
	t.Logf("IDB copies match the bundle: %v", idbMatches(idb, img))
	if _, err := idb.ReadAt(make([]byte, 16), 0); err == nil {
		t.Log("note: an ECC read of a header page worked")
	}
	for _, name := range []string{"u-boot-backup", "u-boot"} {
		p, err := OpenMTD(name)
		if err != nil {
			t.Fatal(err)
		}
		good, err := goodBlocks(p)
		if err != nil {
			t.Fatal(err)
		}
		ub, _ := os.ReadFile(filepath.Join(dir, m.Image("u-boot").File))
		t.Logf("%s: %d good blocks of %d, matches the bundle: %v", name, len(good), p.Size()/p.EraseSize(), partMatches(p, ub))
		p.Close()
	}
}

func TestHWWriteSlotB(t *testing.T) {
	m, dir := hwBundle(t)
	if RunningSlot(readFile("/proc/cmdline")) == "b" {
		t.Skip("slot B is running")
	}
	kernel, rootfs := SlotVolumes("b")
	for _, w := range []struct {
		v      Volume
		target string
	}{{kernel, "kernel"}, {rootfs, "rootfs"}} {
		im := m.Image(w.target)
		res := WriteVolume(UBI0{}, w.v, w.target, filepath.Join(dir, im.File), im)
		t.Logf("%+v", res)
		if !res.Written {
			t.Error(res.Error)
		}
	}
}

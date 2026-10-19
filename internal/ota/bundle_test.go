package ota

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type member struct {
	name string
	data []byte
}

// testBundle returns a valid bundle's members: the manifest, then the images.
func testBundle() (*Manifest, []member) {
	m := &Manifest{Format: 1, Product: Product, Flavour: "debug", Version: "abc1234", BuildID: 202610191900}
	files := map[string][]byte{
		"idbloader": bytes.Repeat([]byte{1}, idbSize),
		"u-boot":    []byte("u-boot"),
		"kernel":    []byte("kernel"),
		"rootfs":    []byte("rootfs"),
	}
	names := map[string]string{"idbloader": "idbloader.img", "u-boot": "u-boot.itb", "kernel": "kernel.itb", "rootfs": "rootfs.ubifs"}
	var ms []member
	for _, t := range Targets {
		d := files[t]
		sum := sha256.Sum256(d)
		m.Images = append(m.Images, Image{t, names[t], int64(len(d)), hex.EncodeToString(sum[:])})
		ms = append(ms, member{names[t], d})
	}
	return m, ms
}

func tarOf(t *testing.T, m any, ms []member) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, d []byte) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(d)), Typeflag: tar.TypeReg})
		tw.Write(d)
	}
	if m != nil {
		b, _ := json.Marshal(m)
		add(ManifestFile, b)
	}
	for _, mb := range ms {
		add(mb.name, mb.data)
	}
	tw.Close()
	return buf.Bytes()
}

func TestReceive(t *testing.T) {
	m, ms := testBundle()
	dir := t.TempDir()
	got, err := Receive(bytes.NewReader(tarOf(t, m, ms)), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "abc1234" || got.Image("kernel").File != "kernel.itb" {
		t.Errorf("manifest %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "rootfs.ubifs")); string(b) != "rootfs" {
		t.Errorf("rootfs %q", b)
	}
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if e, _ := os.ReadDir(dir); len(e) != 0 {
		t.Errorf("%d files after Clear", len(e))
	}
}

func TestReceiveRejects(t *testing.T) {
	cases := map[string]func(m *Manifest, ms []member) (any, []member){
		"not a bundle": func(m *Manifest, ms []member) (any, []member) { return nil, ms },
		"product":      func(m *Manifest, ms []member) (any, []member) { m.Product = "x"; return m, ms },
		"format":       func(m *Manifest, ms []member) (any, []member) { m.Format = 2; return m, ms },
		"flavour":      func(m *Manifest, ms []member) (any, []member) { m.Flavour = "beta"; return m, ms },
		"no image":     func(m *Manifest, ms []member) (any, []member) { m.Images = m.Images[1:]; return m, ms[1:] },
		"path":         func(m *Manifest, ms []member) (any, []member) { m.Images[2].File = "../kernel.itb"; return m, ms },
		"idb size":     func(m *Manifest, ms []member) (any, []member) { m.Images[0].Size = 1; return m, ms },
		"missing":      func(m *Manifest, ms []member) (any, []member) { return m, ms[:3] },
		"extra":        func(m *Manifest, ms []member) (any, []member) { return m, append(ms, member{"x", nil}) },
		"twice":        func(m *Manifest, ms []member) (any, []member) { return m, append(ms, ms[3]) },
		"hash":         func(m *Manifest, ms []member) (any, []member) { ms[2].data = []byte("kernal"); return m, ms },
		"short":        func(m *Manifest, ms []member) (any, []member) { ms[2].data = []byte("kern"); return m, ms },
		"manifest json": func(m *Manifest, ms []member) (any, []member) {
			return json.RawMessage(`{"format": "one"}`), ms
		},
	}
	for name, f := range cases {
		m, ms := testBundle()
		mm, ms2 := f(m, ms)
		if _, err := Receive(bytes.NewReader(tarOf(t, mm, ms2)), t.TempDir()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Receive(strings.NewReader("garbage"), t.TempDir()); err == nil {
		t.Error("garbage accepted")
	}
}

func TestCheckFlavour(t *testing.T) {
	m, _ := testBundle()
	if m.CheckFlavour("release") == nil {
		t.Error("release device took a debug build")
	}
	if m.CheckFlavour("debug") != nil {
		t.Error("debug device refused a debug build")
	}
	m.Flavour = "release"
	if m.CheckFlavour("release") != nil {
		t.Error("release device refused a release build")
	}
}

func TestReceiveRealBundle(t *testing.T) {
	path := os.Getenv("D200_BUNDLE")
	if path == "" {
		t.Skip("set D200_BUNDLE to a bundle to check it")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Receive(f, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s %s %d", m.Version, m.Flavour, m.BuildID)
}

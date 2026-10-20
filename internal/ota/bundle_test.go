package ota

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
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
		m.Images = append(m.Images, Image{Target: t, File: names[t], Size: int64(len(d)), SHA256: hex.EncodeToString(sum[:])})
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
		"two kernels": func(m *Manifest, ms []member) (any, []member) {
			k := m.Images[2]
			k.File = "kernel2.itb"
			m.Images = append(m.Images, k)
			return m, append(ms, member{"kernel2.itb", ms[2].data})
		},
		"hash":  func(m *Manifest, ms []member) (any, []member) { ms[2].data = []byte("kernal"); return m, ms },
		"short": func(m *Manifest, ms []member) (any, []member) { ms[2].data = []byte("kern"); return m, ms },
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
		t.Skip("set D200_BUNDLE to a bundle to check it, and D200_UPDATE_KEY to its key")
	}
	if k := os.Getenv("D200_UPDATE_KEY"); k != "" {
		KeyFile = k
		t.Cleanup(func() { KeyFile = "/etc/d200/update-key.pub" })
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
	t.Logf("%s %s %d, signed %v", m.Version, m.Flavour, m.BuildID, m.Signed)
}

func TestReceiveSigned(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	der, _ := x509.MarshalPKIXPublicKey(pub)
	KeyFile = filepath.Join(t.TempDir(), "update-key.pub")
	t.Cleanup(func() { KeyFile = "/etc/d200/update-key.pub" })
	os.WriteFile(KeyFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o644)

	m, ms := testBundle()
	b, _ := json.Marshal(m)
	_, other, _ := ed25519.GenerateKey(nil)
	signed := func(sig []byte) []member { return append([]member{{SigFile, sig}}, ms...) }
	cases := []struct {
		name    string
		members []member
		signed  bool
	}{
		{"signed", signed(ed25519.Sign(priv, b)), true},
		{"unsigned", ms, false},
		{"another key", signed(ed25519.Sign(other, b)), false},
		{"another manifest", signed(ed25519.Sign(priv, append(b, ' '))), false},
		{"short", signed([]byte("sig")), false},
	}
	for _, tc := range cases {
		got, err := Receive(bytes.NewReader(tarOf(t, m, tc.members)), t.TempDir())
		if err != nil || got.Signed != tc.signed {
			t.Errorf("%s: signed %v, %v", tc.name, got != nil && got.Signed, err)
		}
	}
	os.Remove(KeyFile)
	if got, err := Receive(bytes.NewReader(tarOf(t, m, signed(ed25519.Sign(priv, b)))), t.TempDir()); err != nil || got.Signed {
		t.Errorf("no key: signed %v, %v", got != nil && got.Signed, err)
	}

	for name, members := range map[string][]member{
		"too long": signed(make([]byte, 65)),
		"late":     append(slices.Clone(ms), member{SigFile, ed25519.Sign(priv, b)}),
	} {
		if _, err := Receive(bytes.NewReader(tarOf(t, m, members)), t.TempDir()); err == nil {
			t.Errorf("%s signature accepted", name)
		}
	}
}

func TestValidVersion(t *testing.T) {
	for v, ok := range map[string]bool{
		"v1.2.0": true, "a82a8a8-dirty": true, "1.0+rc_1": true,
		"": false, "v1\n2": false, "v 1": false, strings.Repeat("1", 33): false,
	} {
		if validVersion(v) != ok {
			t.Errorf("%q: %v", v, !ok)
		}
	}
}

// Bundles for newer updaters: an optional image of a target this updater
// doesn't know is checked and skipped; anything else it can't install is
// refused before anything is kept.
func TestReceiveNewerBundles(t *testing.T) {
	extra := []byte("splash")
	sum := sha256.Sum256(extra)
	splash := Image{Target: "splash", File: "splash.img", Size: int64(len(extra)), SHA256: hex.EncodeToString(sum[:])}

	m, ms := testBundle()
	splash.Optional = true
	m.Images = append(m.Images, splash)
	dir := t.TempDir()
	got, err := Receive(bytes.NewReader(tarOf(t, m, append(ms, member{"splash.img", extra}))), dir)
	if err != nil {
		t.Fatalf("optional image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "splash.img")); err == nil || got.Image("kernel").File != "kernel.itb" {
		t.Error("the optional image was kept, or the rest lost")
	}

	m, ms = testBundle()
	splash.Optional = false
	m.Images = append(m.Images, splash)
	if _, err := Receive(bytes.NewReader(tarOf(t, m, append(ms, member{"splash.img", extra}))), t.TempDir()); err == nil ||
		err.Error() != "This update needs newer device software. Install a newer update first." {
		t.Errorf("required unknown image: %v", err)
	}

	m, ms = testBundle()
	m.MinUpdater, m.MinUpdaterRelease = UpdaterVersion+1, "v0.3.0"
	dir = t.TempDir()
	if _, err := Receive(bytes.NewReader(tarOf(t, m, ms)), dir); err == nil ||
		err.Error() != "This update needs newer device software. Install v0.3.0 first." {
		t.Errorf("min_updater: %v", err)
	}
	if e, _ := os.ReadDir(dir); len(e) != 0 {
		t.Errorf("kept %d files of a refused bundle", len(e))
	}

	// fields this updater doesn't know are ignored; min_updater it meets is fine
	m, ms = testBundle()
	m.MinUpdater = UpdaterVersion
	b, _ := json.Marshal(m)
	b = append(b[:len(b)-1], `,"compression":"none","images_note":1}`...)
	var raw json.RawMessage = b
	if _, err := Receive(bytes.NewReader(tarOf(t, raw, ms)), t.TempDir()); err != nil {
		t.Errorf("unknown manifest fields: %v", err)
	}
}

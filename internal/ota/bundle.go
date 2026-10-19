// Package ota installs update bundles on the D200, following d200-buildroot's
// docs/ota.md: receive and check a bundle, write the bootloader and the
// inactive slot, and switch slots.
package ota

import (
	"archive/tar"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// Targets are what a bundle writes, in write order.
var Targets = []string{"idbloader", "u-boot", "kernel", "rootfs"}

// Manifest describes a bundle (manifest.json).
type Manifest struct {
	Format  int     `json:"format"`
	Product string  `json:"product"`
	Flavour string  `json:"flavour"`
	Version string  `json:"version"`
	BuildID int64   `json:"build_id"` // UTC YYYYMMDDHHMM, as os-release's BUILD_ID
	Images  []Image `json:"images"`

	// Signed is set by Receive: the bundle's signature checks out with
	// KeyFile, so it's an official build.
	Signed bool `json:"-"`
}

// Image is one file of a bundle.
type Image struct {
	Target string `json:"target"`
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Image returns the bundle's image for target.
func (m *Manifest) Image(target string) Image {
	for _, im := range m.Images {
		if im.Target == target {
			return im
		}
	}
	return Image{}
}

const (
	// Product is the device this program updates, as bundles name it.
	Product = "d200"

	ManifestFile = "manifest.json"
	// SigFile, if present, comes straight after the manifest: an Ed25519
	// signature of manifest.json, which holds every image's sha256.
	SigFile     = "manifest.sig"
	maxManifest = 64 << 10
	maxImage    = 64 << 20
	idbSize     = 256 << 10 // one IDB copy
)

// KeyFile is the public key of official builds (PEM, PKIX, Ed25519). A fork
// puts its own here.
var KeyFile = "/etc/d200/update-key.pub"

// check reports what's wrong with the manifest, if anything.
func (m *Manifest) check() error {
	if m.Format != 1 || m.Product != Product {
		return fmt.Errorf("not an update for this device (format %d, product %q)", m.Format, m.Product)
	}
	if m.Version == "" || m.BuildID <= 0 || (m.Flavour != "release" && m.Flavour != "debug") {
		return errors.New("manifest: missing version, build_id or flavour")
	}
	if len(m.Images) != len(Targets) {
		return fmt.Errorf("manifest: %d images, want %d", len(m.Images), len(Targets))
	}
	var files []string
	for _, t := range Targets {
		im := m.Image(t)
		switch {
		case im.Target == "":
			return fmt.Errorf("manifest: no %s image", t)
		case !validFile(im.File) || im.File == ManifestFile || im.File == SigFile || slices.Contains(files, im.File):
			return fmt.Errorf("manifest: bad file name %q", im.File)
		case im.Size <= 0 || im.Size > maxImage || (t == "idbloader" && im.Size != idbSize):
			return fmt.Errorf("manifest: bad size for %s", im.File)
		case len(im.SHA256) != 64:
			return fmt.Errorf("manifest: bad sha256 for %s", im.File)
		}
		files = append(files, im.File)
	}
	return nil
}

// validFile reports whether name is a plain file name.
func validFile(name string) bool {
	if name == "" || len(name) > 64 || name[0] == '.' {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// Receive reads a bundle (a tar) from r into dir, one file per member, and
// checks it: the manifest, and every listed file present once with its size
// and sha256, and nothing else but an optional signature.
func Receive(r io.Reader, dir string) (*Manifest, error) {
	tr := tar.NewReader(r)
	h, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("not a bundle: %w", err)
	}
	if h.Name != ManifestFile || h.Size > maxManifest {
		return nil, errors.New("not a bundle: manifest.json must come first")
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := m.check(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), b, 0o644); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	for first := true; ; first = false {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if first && h.Name == SigFile && h.Typeflag == tar.TypeReg && h.Size <= ed25519.SignatureSize {
			sig, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			m.Signed = verify(b, sig)
			continue
		}
		i := slices.IndexFunc(m.Images, func(im Image) bool { return im.File == h.Name })
		if i < 0 || seen[h.Name] || h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("unexpected %q in the bundle", h.Name)
		}
		seen[h.Name] = true
		if err := save(tr, filepath.Join(dir, h.Name), m.Images[i]); err != nil {
			return nil, err
		}
	}
	for _, im := range m.Images {
		if !seen[im.File] {
			return nil, fmt.Errorf("%s is missing from the bundle", im.File)
		}
	}
	return &m, nil
}

// verify reports whether sig signs manifest with KeyFile's key.
func verify(manifest, sig []byte) bool {
	b, err := os.ReadFile(KeyFile)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PUBLIC KEY" {
		return false
	}
	key, err := x509.ParsePKIXPublicKey(blk.Bytes)
	pub, ok := key.(ed25519.PublicKey)
	return err == nil && ok && ed25519.Verify(pub, manifest, sig)
}

// save writes r to path, checking it against im.
func save(r io.Reader, path string, im Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, im.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return err
	case n != im.Size:
		return fmt.Errorf("%s: %d bytes, want %d", im.File, n, im.Size)
	case hex.EncodeToString(h.Sum(nil)) != im.SHA256:
		return fmt.Errorf("%s: sha256 doesn't match", im.File)
	}
	return nil
}

// CheckFlavour refuses a debug bundle on a release device.
func (m *Manifest) CheckFlavour(device string) error {
	if device == "release" && m.Flavour != "release" {
		return fmt.Errorf("a release device can't install a %s build", m.Flavour)
	}
	return nil
}

// Clear empties dir.
func Clear(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

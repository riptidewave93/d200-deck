package osrelease

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "os-release")
	os.WriteFile(p, []byte(`NAME="D200"
# comment
PRETTY_NAME="Ulanzi D200 v0.1.0 (release)"
VERSION_ID=v0.1.0
VARIANT_ID=release
BUILD_ID="202610191312"
broken line
`), 0o644)
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	want := Info{Name: "Ulanzi D200 v0.1.0 (release)", Version: "v0.1.0", Flavour: "release", Build: "202610191312"}
	if got != want {
		t.Errorf("got %+v", got)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("no error for a missing file")
	}
}

package serial

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead(t *testing.T) {
	old := Path
	defer func() { Path = old }()
	Path = filepath.Join(t.TempDir(), "serial-number")
	os.WriteFile(Path, []byte("00112233445566aa\x00"), 0o644)
	if s, err := Read(); err != nil || s != "00112233445566aa" {
		t.Errorf("%q, %v", s, err)
	}
	os.WriteFile(Path, []byte("\x00"), 0o644)
	if _, err := Read(); err == nil {
		t.Error("read an empty serial")
	}
	os.Remove(Path)
	if _, err := Read(); err == nil {
		t.Error("read a missing serial")
	}
}

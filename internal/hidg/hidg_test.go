package hidg

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/riptidewave93/d200-deck/proto"
)

// fifo returns a named pipe and its read end.
func fifo(t *testing.T) (string, *os.File) {
	p := filepath.Join(t.TempDir(), "hidg")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return p, r
}

func read(t *testing.T, r *os.File, n int) []byte {
	b := make([]byte, n)
	got, err := r.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	return b[:got]
}

func TestReports(t *testing.T) {
	p, r := fifo(t)
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	d.Keyboard(0x03, 0x04, 0x05)
	if got := read(t, r, 64); !bytes.Equal(got, []byte{ReportKeyboard, 0x03, 0, 0x04, 0x05, 0, 0, 0, 0}) {
		t.Errorf("keyboard % x", got)
	}
	d.Consumer(0x00e9)
	if got := read(t, r, 64); !bytes.Equal(got, []byte{ReportConsumer, 0xe9, 0x00}) {
		t.Errorf("consumer % x", got)
	}
	d.System(2) // sleep
	if got := read(t, r, 64); !bytes.Equal(got, []byte{ReportSystem, 0x02}) {
		t.Errorf("system % x", got)
	}
	d.Mouse(1, -1, 2, -3)
	if got := read(t, r, 64); !bytes.Equal(got, []byte{ReportMouse, 1, 0xff, 2, 0xfd}) {
		t.Errorf("mouse % x", got)
	}
}

func TestSendTimesOut(t *testing.T) {
	p, _ := fifo(t)
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// nobody reads: the pipe fills, then sends give up instead of blocking
	for range 100000 {
		if err := d.Keyboard(0); err != nil {
			return
		}
	}
	t.Error("sends never failed")
}

func TestRaw(t *testing.T) {
	p, r := fifo(t)
	raw, err := OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err := raw.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	got := read(t, r, 2*proto.RawInSize)
	if len(got) != proto.RawInSize || !bytes.Equal(got[:4], []byte{1, 2, 3, 0}) {
		t.Errorf("raw report % x", got)
	}
	w, _ := os.OpenFile(p, os.O_WRONLY, 0)
	defer w.Close()
	w.Write([]byte("request"))
	buf := make([]byte, 64)
	n, err := raw.Read(buf)
	if err != nil || string(buf[:n]) != "request" {
		t.Errorf("read %q, %v", buf[:n], err)
	}
}

func TestOpenMissing(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("opened a missing gadget")
	}
	if _, err := OpenRaw(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("opened a missing channel")
	}
}

func TestDescriptors(t *testing.T) {
	if len(ReportDescriptor) == 0 || ReportDescriptor[len(ReportDescriptor)-1] != 0xc0 {
		t.Error("report descriptor doesn't end a collection")
	}
}

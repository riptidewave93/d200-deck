package hidg

import (
	"os"
	"syscall"

	"github.com/riptidewave93/d200-deck/proto"
)

// Raw is the config channel gadget (/dev/hidg1).
type Raw struct{ f *os.File }

// OpenRaw opens the config channel.
func OpenRaw(path string) (*Raw, error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return &Raw{f: f}, nil
}

// Read waits for the next output report from the host, one per call. The
// gadget drops the oldest unread reports, so the host must wait for a reply
// before sending the next one.
func (r *Raw) Read(buf []byte) (int, error) {
	return r.f.Read(buf)
}

// Write sends one input report, padded to proto.RawInSize.
func (r *Raw) Write(report []byte) error {
	var b [proto.RawInSize]byte
	copy(b[:], report)
	return write(r.f, b[:], "config channel reports")
}

// Close closes the channel.
func (r *Raw) Close() error { return r.f.Close() }

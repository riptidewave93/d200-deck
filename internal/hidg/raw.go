package hidg

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/riptidewave93/d200-deck/proto"
)

// RawUsagePage is the vendor usage page that identifies the config channel.
const RawUsagePage = 0xffd2

// RawDescriptor is the config channel's report descriptor: one vendor
// collection, no report IDs, 64-byte input and 1024-byte output reports.
// Frozen: hosts identify the channel by it.
var RawDescriptor = []byte{
	0x06, RawUsagePage & 0xff, RawUsagePage >> 8, // Usage Page (vendor)
	0x09, 0x01, // Usage (1)
	0xa1, 0x01, // Collection (Application)
	0x15, 0x00, 0x26, 0xff, 0x00, 0x75, 0x08, // bytes
	0x09, 0x02, 0x95, proto.RawInSize, 0x81, 0x02, // Input
	0x09, 0x03, 0x96, proto.RawOutSize & 0xff, proto.RawOutSize >> 8, 0x91, 0x02, // Output
	0xc0,
}

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

// Read waits for the next output report from the host. The gadget keeps only
// the latest unread report, so the host must wait for a reply before sending
// the next one.
func (r *Raw) Read(buf []byte) (int, error) {
	return r.f.Read(buf)
}

// Write sends one input report, padded to proto.RawInSize.
func (r *Raw) Write(report []byte) error {
	var b [proto.RawInSize]byte
	copy(b[:], report)
	deadline := time.Now().Add(sendTimeout)
	for {
		_, err := r.f.Write(b[:])
		if !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("USB host not reading config channel reports")
		}
		time.Sleep(time.Millisecond)
	}
}

// Close closes the channel.
func (r *Raw) Close() error { return r.f.Close() }

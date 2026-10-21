package hidraw

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <stdint.h>

int hid_list(uint64_t *ids, int max);
int hid_descriptor(uint64_t id, uint8_t *buf, int size);
void *hid_open(uint64_t id, int size, uintptr_t ctx, int *err);
void hid_run(void *h);
void hid_stop(void *h);
int hid_write(void *h, uint8_t *b, int n);
void hid_close(void *h);
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"runtime/cgo"
	"slices"
	"strconv"
	"sync"
	"unsafe"
)

// FindAll returns the paths, IORegistry entry IDs, of the HID devices whose
// report descriptor is one of descs. Reading descriptors opens nothing, so
// it needs no permission, even for keyboards.
func FindAll(descs ...[]byte) []string {
	ids := make([]C.uint64_t, 512)
	n := int(C.hid_list(&ids[0], C.int(len(ids))))
	var paths []string
	for _, id := range ids[:min(n, len(ids))] {
		d := descriptor(uint64(id))
		if d != nil && slices.ContainsFunc(descs, func(want []byte) bool { return bytes.Equal(d, want) }) {
			paths = append(paths, strconv.FormatUint(uint64(id), 10))
		}
	}
	return paths
}

func descriptor(id uint64) []byte {
	buf := make([]byte, 4096)
	n := C.hid_descriptor(C.uint64_t(id), (*C.uint8_t)(&buf[0]), C.int(len(buf)))
	if n < 0 {
		return nil
	}
	return buf[:n]
}

// Device is an open HID device.
type Device struct {
	h       unsafe.Pointer // the C side's state
	handle  cgo.Handle     // how the C side's callbacks find this Device
	reports chan []byte
	gone    chan struct{} // closed when the device is unplugged or closed
	ran     chan struct{} // closed when the run loop has stopped
	mu      sync.RWMutex  // held (read) during writes, (write) to close

	goneOnce, closeOnce sync.Once
}

var errClosed = errors.New("device closed")

// Open opens a HID device by its path from FindAll.
func Open(path string) (*Device, error) {
	id, err := strconv.ParseUint(path, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%q isn't a HID device path", path)
	}
	desc := descriptor(id)
	if desc == nil {
		return nil, fmt.Errorf("no HID device %s", path)
	}
	d := &Device{reports: make(chan []byte, 64), gone: make(chan struct{}), ran: make(chan struct{})}
	d.handle = cgo.NewHandle(d)
	var ret C.int
	d.h = C.hid_open(C.uint64_t(id), C.int(max(capsOf(desc).in, 64)), C.uintptr_t(d.handle), &ret)
	if d.h == nil {
		d.handle.Delete()
		return nil, fmt.Errorf("opening HID device %s: IOReturn %#x", path, uint32(ret))
	}
	go func() {
		// IOKit calls back on the thread that runs the device's run loop
		runtime.LockOSThread()
		C.hid_run(d.h)
		close(d.ran)
	}()
	return d, nil
}

//export goInputReport
func goInputReport(ctx C.uintptr_t, report *C.uint8_t, n C.int) {
	d := cgo.Handle(ctx).Value().(*Device)
	select {
	case d.reports <- C.GoBytes(unsafe.Pointer(report), n):
	default: // nobody reading: drop it, as hidraw does
	}
}

//export goRemoved
func goRemoved(ctx C.uintptr_t) { cgo.Handle(ctx).Value().(*Device).markGone() }

func (d *Device) markGone() { d.goneOnce.Do(func() { close(d.gone) }) }

// WriteReport sends an output report.
func (d *Device) WriteReport(b []byte) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	select {
	case <-d.gone:
		return errClosed
	default:
	}
	if len(b) == 0 {
		return errors.New("empty report")
	}
	if r := C.hid_write(d.h, (*C.uint8_t)(&b[0]), C.int(len(b))); r != 0 {
		return fmt.Errorf("writing a report: IOReturn %#x", uint32(r))
	}
	return nil
}

// ReadReport waits for one input report.
func (d *Device) ReadReport(b []byte) (int, error) {
	select {
	case r := <-d.reports:
		return copy(b, r), nil
	case <-d.gone:
		return 0, errClosed
	}
}

// Close closes the device, which also ends a waiting ReadReport.
func (d *Device) Close() error {
	d.closeOnce.Do(func() {
		d.markGone()
		C.hid_stop(d.h)
		<-d.ran
		d.mu.Lock()
		C.hid_close(d.h)
		d.mu.Unlock()
		d.handle.Delete()
	})
	return nil
}

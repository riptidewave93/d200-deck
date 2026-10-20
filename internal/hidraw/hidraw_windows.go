package hidraw

import (
	"errors"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	hidDLL               = windows.NewLazySystemDLL("hid.dll")
	procGetHidGuid       = hidDLL.NewProc("HidD_GetHidGuid")
	procGetPreparsedData = hidDLL.NewProc("HidD_GetPreparsedData")
	procFreePreparsed    = hidDLL.NewProc("HidD_FreePreparsedData")
	procGetCaps          = hidDLL.NewProc("HidP_GetCaps")
)

// hidpCaps is HIDP_CAPS.
type hidpCaps struct {
	Usage, UsagePage                                                       uint16
	InputReportByteLength, OutputReportByteLength, FeatureReportByteLength uint16
	_                                                                      [17]uint16
	_                                                                      [10]uint16
}

const hidpStatusSuccess = 0x00110000

// FindAll returns the device paths of the HID devices that match one of
// descs: the same usage page and usage and report sizes.
func FindAll(descs ...[]byte) []string {
	var want []caps
	for _, d := range descs {
		want = append(want, capsOf(d))
	}
	var guid windows.GUID
	procGetHidGuid.Call(uintptr(unsafe.Pointer(&guid)))
	paths, err := windows.CM_Get_Device_Interface_List("", &guid, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil
	}
	var found []string
	for _, p := range paths {
		// no access rights needed to read caps, even of devices Windows keeps
		// to itself (keyboards, mice)
		h, err := createFile(p, 0)
		if err != nil {
			continue
		}
		c, err := getCaps(h)
		windows.CloseHandle(h)
		if err == nil && slices.Contains(want, c) {
			found = append(found, p)
		}
	}
	sort.Strings(found)
	return found
}

func createFile(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
}

// getCaps reads a device's caps; report sizes on Windows include the report
// ID byte, which ours don't have.
func getCaps(h windows.Handle) (caps, error) {
	var pd uintptr
	if ok, _, _ := procGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pd))); ok == 0 {
		return caps{}, errors.New("HidD_GetPreparsedData failed")
	}
	defer procFreePreparsed.Call(pd)
	var c hidpCaps
	if st, _, _ := procGetCaps.Call(pd, uintptr(unsafe.Pointer(&c))); st != hidpStatusSuccess {
		return caps{}, errors.New("HidP_GetCaps failed")
	}
	return caps{c.UsagePage, c.Usage, int(c.InputReportByteLength) - 1, int(c.OutputReportByteLength) - 1}, nil
}

// Device is an open HID device.
type Device struct {
	h       windows.Handle
	in, out int
	mu      sync.RWMutex // held (read) during I/O, (write) to close
	closed  atomic.Bool
}

// Open opens a HID device.
func Open(path string) (*Device, error) {
	h, err := createFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		return nil, err
	}
	c, err := getCaps(h)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &Device{h: h, in: c.in, out: c.out}, nil
}

var errClosed = errors.New("device closed")

// io runs one overlapped read or write and waits for it.
func (d *Device) io(write bool, b []byte) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed.Load() {
		return 0, errClosed
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(ev)
	ov := windows.Overlapped{HEvent: ev}
	var n uint32
	if write {
		err = windows.WriteFile(d.h, b, &n, &ov)
	} else {
		err = windows.ReadFile(d.h, b, &n, &ov)
	}
	if err == windows.ERROR_IO_PENDING {
		err = windows.GetOverlappedResult(d.h, &ov, &n, true)
	}
	if err != nil && d.closed.Load() {
		err = errClosed
	}
	return int(n), err
}

// WriteReport sends an output report. The device has no report IDs, so
// Windows wants a leading 0.
func (d *Device) WriteReport(b []byte) error {
	buf := make([]byte, d.out+1)
	copy(buf[1:], b)
	_, err := d.io(true, buf)
	return err
}

// ReadReport waits for one input report.
func (d *Device) ReadReport(b []byte) (int, error) {
	buf := make([]byte, d.in+1)
	n, err := d.io(false, buf)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	return copy(b, buf[1:n]), nil
}

// Close closes the device, which also ends a waiting ReadReport.
func (d *Device) Close() error {
	if d.closed.Swap(true) {
		return nil
	}
	// cancel I/O until none is left running (one may start just after a cancel)
	for {
		windows.CancelIoEx(d.h, nil)
		if d.mu.TryLock() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer d.mu.Unlock()
	return windows.CloseHandle(d.h)
}

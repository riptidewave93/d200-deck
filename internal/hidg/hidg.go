// Package hidg sends reports through the USB HID gadget (/dev/hidg0).
//
// The gadget advertises one fixed set of HID devices, whatever the current
// configuration uses, so the USB device never has to change (and re-connect)
// when the configuration does. ReportDescriptor is that set; the gadget setup
// writes it to configfs (deckd hid-descriptor), and the methods below build
// the matching reports.
package hidg

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Report IDs, one per device in ReportDescriptor.
const (
	ReportKeyboard = 1
	ReportConsumer = 2
	ReportSystem   = 3
	ReportMouse    = 4
)

// ReportLength is the longest report, including its report ID (keyboard).
const ReportLength = 9

// ReportDescriptor declares a keyboard, a consumer control, a system control
// and a mouse.
var ReportDescriptor = []byte{
	// Keyboard: modifier bits, a reserved byte, 6 key codes (up to 0xa4, so
	// F13-F24 too); LED output report
	0x05, 0x01, 0x09, 0x06, 0xa1, 0x01, 0x85, ReportKeyboard,
	0x05, 0x07, 0x19, 0xe0, 0x29, 0xe7, 0x15, 0x00, 0x25, 0x01, 0x75, 0x01, 0x95, 0x08, 0x81, 0x02,
	0x95, 0x01, 0x75, 0x08, 0x81, 0x03,
	0x95, 0x05, 0x75, 0x01, 0x05, 0x08, 0x19, 0x01, 0x29, 0x05, 0x91, 0x02,
	0x95, 0x01, 0x75, 0x03, 0x91, 0x03,
	0x95, 0x06, 0x75, 0x08, 0x15, 0x00, 0x26, 0xa4, 0x00, 0x05, 0x07, 0x19, 0x00, 0x29, 0xa4, 0x81, 0x00,
	0xc0,
	// Consumer control: one 16-bit usage (media keys, volume, app launch, ...)
	0x05, 0x0c, 0x09, 0x01, 0xa1, 0x01, 0x85, ReportConsumer,
	0x15, 0x00, 0x26, 0xff, 0x03, 0x19, 0x00, 0x2a, 0xff, 0x03, 0x75, 0x10, 0x95, 0x01, 0x81, 0x00,
	0xc0,
	// System control: power down, sleep, wake up bits
	0x05, 0x01, 0x09, 0x80, 0xa1, 0x01, 0x85, ReportSystem,
	0x19, 0x81, 0x29, 0x83, 0x15, 0x00, 0x25, 0x01, 0x75, 0x01, 0x95, 0x03, 0x81, 0x02,
	0x95, 0x05, 0x81, 0x01,
	0xc0,
	// Mouse: 5 buttons, relative X, Y and wheel
	0x05, 0x01, 0x09, 0x02, 0xa1, 0x01, 0x85, ReportMouse,
	0x09, 0x01, 0xa1, 0x00,
	0x05, 0x09, 0x19, 0x01, 0x29, 0x05, 0x15, 0x00, 0x25, 0x01, 0x95, 0x05, 0x75, 0x01, 0x81, 0x02,
	0x95, 0x01, 0x75, 0x03, 0x81, 0x01,
	0x05, 0x01, 0x09, 0x30, 0x09, 0x31, 0x09, 0x38, 0x15, 0x81, 0x25, 0x7f, 0x75, 0x08, 0x95, 0x03,
	0x81, 0x06,
	0xc0, 0xc0,
}

// System control bits.
const (
	SystemPowerDown = 1 << 0
	SystemSleep     = 1 << 1
	SystemWakeUp    = 1 << 2
)

// Device is an open HID gadget.
type Device struct{ f *os.File }

// Open opens the gadget. A write waits at most sendTimeout for the PC to take
// the previous report, so nothing blocks when no PC is listening.
func Open(path string) (*Device, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return &Device{f: f}, nil
}

const sendTimeout = 50 * time.Millisecond

func (d *Device) send(report ...byte) error {
	deadline := time.Now().Add(sendTimeout)
	for {
		_, err := d.f.Write(report)
		if !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("USB host not reading HID reports")
		}
		time.Sleep(time.Millisecond)
	}
}

// Keyboard reports the modifier bits and up to 6 pressed key codes (USB HID
// keyboard usages); no keys releases everything.
func (d *Device) Keyboard(modifiers byte, keys ...byte) error {
	r := []byte{ReportKeyboard, modifiers, 0, 0, 0, 0, 0, 0, 0}
	copy(r[3:], keys)
	return d.send(r...)
}

// Consumer reports usage as pressed; usage 0 releases it.
func (d *Device) Consumer(usage uint16) error {
	return d.send(ReportConsumer, byte(usage), byte(usage>>8))
}

// System reports the system control bits (SystemSleep, ...); 0 releases.
func (d *Device) System(bits byte) error {
	return d.send(ReportSystem, bits)
}

// Mouse reports buttons (bit 0 = left) and relative movement.
func (d *Device) Mouse(buttons byte, dx, dy, wheel int8) error {
	return d.send(ReportMouse, buttons, byte(dx), byte(dy), byte(wheel))
}

// Close closes the gadget.
func (d *Device) Close() error { return d.f.Close() }

//go:build !linux && !windows

package hidraw

import "errors"

var errUnsupported = errors.New("HID devices aren't supported on this system yet")

// Device is an open HID device.
type Device struct{}

// FindAll finds no devices.
func FindAll(descs ...[]byte) []string { return nil }

// Open fails.
func Open(path string) (*Device, error) { return nil, errUnsupported }

// WriteReport fails.
func (d *Device) WriteReport(b []byte) error { return errUnsupported }

// ReadReport fails.
func (d *Device) ReadReport(b []byte) (int, error) { return 0, errUnsupported }

// Close does nothing.
func (d *Device) Close() error { return nil }

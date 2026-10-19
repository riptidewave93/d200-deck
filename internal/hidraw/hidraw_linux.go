// Package hidraw finds and opens the D200's config channel through Linux
// hidraw.
package hidraw

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Device is an open hidraw node.
type Device struct {
	f       *os.File
	timeout time.Duration
}

// Find returns the /dev path of the first hidraw node whose report descriptor
// starts with prefix (e.g. the config channel's vendor usage page).
func Find(prefix []byte) (string, error) {
	nodes, _ := filepath.Glob("/sys/class/hidraw/hidraw*")
	for _, n := range nodes {
		d, err := os.ReadFile(filepath.Join(n, "device", "report_descriptor"))
		if err == nil && bytes.HasPrefix(d, prefix) {
			return "/dev/" + filepath.Base(n), nil
		}
	}
	return "", errors.New("no D200 config channel found")
}

// Open opens a hidraw node; reads time out after timeout.
func Open(path string, timeout time.Duration) (*Device, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &Device{f: f, timeout: timeout}, nil
}

// WriteReport sends an output report. The device has no report IDs, so
// hidraw wants a leading 0.
func (d *Device) WriteReport(b []byte) error {
	_, err := d.f.Write(append([]byte{0}, b...))
	return err
}

// ReadReport receives one input report.
func (d *Device) ReadReport(b []byte) (int, error) {
	d.f.SetReadDeadline(time.Now().Add(d.timeout))
	return d.f.Read(b)
}

// Close closes the node.
func (d *Device) Close() error { return d.f.Close() }

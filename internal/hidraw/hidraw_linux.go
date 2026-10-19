package hidraw

import (
	"bytes"
	"os"
	"path/filepath"
)

// Device is an open hidraw node.
type Device struct{ f *os.File }

// sysHidraw is where the kernel lists hidraw nodes.
var sysHidraw = "/sys/class/hidraw"

// FindAll returns the /dev paths of the hidraw nodes whose report descriptor
// is desc.
func FindAll(desc []byte) []string {
	var paths []string
	nodes, _ := filepath.Glob(sysHidraw + "/hidraw*")
	for _, n := range nodes {
		d, err := os.ReadFile(filepath.Join(n, "device", "report_descriptor"))
		if err == nil && bytes.Equal(d, desc) {
			paths = append(paths, "/dev/"+filepath.Base(n))
		}
	}
	return paths
}

// Open opens a hidraw node.
func Open(path string) (*Device, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &Device{f: f}, nil
}

// WriteReport sends an output report. The device has no report IDs, so
// hidraw wants a leading 0.
func (d *Device) WriteReport(b []byte) error {
	_, err := d.f.Write(append([]byte{0}, b...))
	return err
}

// ReadReport waits for one input report.
func (d *Device) ReadReport(b []byte) (int, error) { return d.f.Read(b) }

// Close closes the node, which also ends a waiting ReadReport.
func (d *Device) Close() error { return d.f.Close() }

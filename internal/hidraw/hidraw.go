// Package hidraw finds and opens the D200's config channel: Linux hidraw
// nodes, Windows HID devices, or macOS IOKit HID devices.
package hidraw

// caps are what a report descriptor says about its device, as far as
// telling the config channel apart: Windows reports these, not the
// descriptor.
type caps struct {
	usagePage, usage uint16
	in, out          int // report sizes in bytes, without a report ID
}

// capsOf reads caps from a simple report descriptor (one collection, no
// report IDs, like proto.RawDescriptor).
func capsOf(desc []byte) caps {
	var c caps
	count := 0
	for i := 0; i < len(desc); {
		prefix := desc[i]
		size := int(prefix & 3)
		if size == 3 {
			size = 4
		}
		if i+1+size > len(desc) {
			break
		}
		v := 0
		for j := range size {
			v |= int(desc[i+1+j]) << (8 * j)
		}
		switch prefix &^ 3 {
		case 0x04: // Usage Page
			c.usagePage = uint16(v)
		case 0x08: // Usage
			if c.usage == 0 {
				c.usage = uint16(v)
			}
		case 0x94: // Report Count
			count = v
		case 0x80: // Input
			c.in += count
		case 0x90: // Output
			c.out += count
		}
		i += 1 + size
	}
	return c
}

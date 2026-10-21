package hidraw

import (
	"testing"

	"github.com/riptidewave93/d200-deck/proto"
)

// Without a D200 plugged in: enumeration works and finds none.
func TestFindAllNone(t *testing.T) {
	if paths := FindAll(proto.RawDescriptor); len(paths) != 0 {
		t.Skipf("a D200 is plugged in: %q", paths)
	}
	for _, p := range []string{"", "x", "0", "18446744073709551615"} {
		if _, err := Open(p); err == nil {
			t.Errorf("Open(%q) worked", p)
		}
	}
}

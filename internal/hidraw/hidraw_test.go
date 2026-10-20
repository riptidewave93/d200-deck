package hidraw

import (
	"testing"

	"github.com/riptidewave93/d200-deck/proto"
)

func TestCapsOf(t *testing.T) {
	want := caps{usagePage: 0xffd2, usage: 1, in: proto.RawInSize, out: proto.RawOutSize}
	if got := capsOf(proto.RawDescriptor); got != want {
		t.Errorf("capsOf = %+v, want %+v", got, want)
	}
	want.in = proto.RawInSizeV3
	if got := capsOf(proto.RawDescriptorV3); got != want {
		t.Errorf("capsOf(v3) = %+v, want %+v", got, want)
	}
	if got := capsOf([]byte{0x06, 0xd2}); got != (caps{}) { // cut short
		t.Errorf("capsOf(short) = %+v", got)
	}
}

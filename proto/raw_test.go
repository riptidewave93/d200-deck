package proto

import (
	"bytes"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, MaxRequest} {
		payload := bytes.Repeat([]byte{0xab}, n)
		b, err := EncodeRequest(0x1234, CmdHello, payload)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != RawOutSize {
			t.Fatalf("report is %d bytes", len(b))
		}
		req, err := DecodeRequest(b)
		if err != nil {
			t.Fatal(err)
		}
		if req.Seq != 0x1234 || req.Cmd != CmdHello || !bytes.Equal(req.Payload, payload) {
			t.Errorf("%d bytes: got %+v", n, req)
		}
	}
	if _, err := EncodeRequest(1, 1, make([]byte, MaxRequest+1)); err == nil {
		t.Error("encoded an oversized request")
	}
}

func TestDecodeRequestErrors(t *testing.T) {
	for _, b := range [][]byte{nil, {1, 2, 3, 4}, {1, 0, 1, 10, 0, 0}} {
		if _, err := DecodeRequest(b); err == nil {
			t.Errorf("decoded %v", b)
		}
	}
}

func TestReplySplit(t *testing.T) {
	for _, n := range []int{0, 1, MaxReply, MaxReply + 1, 5*MaxReply + 3} {
		payload := make([]byte, n)
		for i := range payload {
			payload[i] = byte(i)
		}
		parts := EncodeReply(0xbeef, StatusOK, payload)
		want := max(1, (n+MaxReply-1)/MaxReply)
		if len(parts) != want {
			t.Fatalf("%d bytes: %d reports, want %d", n, len(parts), want)
		}
		var got []byte
		for i, b := range parts {
			if len(b) != RawInSize {
				t.Fatalf("report is %d bytes", len(b))
			}
			p, err := DecodeReply(b)
			if err != nil {
				t.Fatal(err)
			}
			if p.Seq != 0xbeef || p.Status != StatusOK || p.First != (i == 0) || p.More != (i < len(parts)-1) {
				t.Errorf("report %d: %+v", i, p)
			}
			got = append(got, p.Payload...)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("%d bytes: payload changed", n)
		}
	}
}

func TestDecodeReplyErrors(t *testing.T) {
	long := make([]byte, RawInSize)
	long[4], long[5] = 0xff, 0x03 // 1023 bytes in a 1024-byte report
	for _, b := range [][]byte{nil, {1, 0, 0, 0}, {1, 0, 0, 0, 5, 1}, long} {
		if _, err := DecodeReply(b); err == nil {
			t.Errorf("decoded %v", b)
		}
	}
}

func TestRawDescriptorFrozen(t *testing.T) {
	// hosts find the channel by these exact bytes; changing them hides every deck
	want := []byte{
		0x06, 0xd2, 0xff, 0x09, 0x01, 0xa1, 0x01, 0x15, 0x00, 0x26, 0xff, 0x00, 0x75, 0x08,
		0x09, 0x02, 0x96, 0x00, 0x04, 0x81, 0x02, 0x09, 0x03, 0x96, 0x00, 0x04, 0x91, 0x02, 0xc0,
	}
	if !bytes.Equal(RawDescriptor, want) {
		t.Errorf("RawDescriptor = % x", RawDescriptor)
	}
}

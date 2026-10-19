package proto

import (
	"bytes"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, MaxRequest} {
		payload := bytes.Repeat([]byte{0xab}, n)
		b, err := EncodeRequest(7, CmdHello, payload)
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
		if req.Seq != 7 || req.Cmd != CmdHello || !bytes.Equal(req.Payload, payload) {
			t.Errorf("%d bytes: got %+v", n, req)
		}
	}
	if _, err := EncodeRequest(1, 1, make([]byte, MaxRequest+1)); err == nil {
		t.Error("encoded an oversized request")
	}
}

func TestDecodeRequestErrors(t *testing.T) {
	for _, b := range [][]byte{nil, {1, 2, 3}, {1, 1, 10, 0, 0}} {
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
		parts := EncodeReply(9, StatusOK, payload)
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
			if p.Seq != 9 || p.Status != StatusOK || p.More != (i < len(parts)-1) {
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
	for _, b := range [][]byte{nil, {1, 0, 0}, {1, 0, 0, 5, 1}} {
		if _, err := DecodeReply(b); err == nil {
			t.Errorf("decoded %v", b)
		}
	}
}

package deckd

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"testing"

	"github.com/riptidewave93/d200-deck/proto"
)

func TestHostLooks(t *testing.T) {
	d := newTestDeckd() // the default config: now playing on key 4
	beat := func(host string) proto.ActionHostReply {
		b, _ := json.Marshal(proto.ActionHost{ID: host})
		s, reply := d.handle(proto.Request{Cmd: proto.CmdActionHost, Payload: b})
		var r proto.ActionHostReply
		if s != proto.StatusOK || json.Unmarshal(reply, &r) != nil {
			t.Fatalf("heartbeat: %d %s", s, reply)
		}
		return r
	}
	look := func(l proto.HostLook) byte {
		b, _ := json.Marshal(l)
		s, _ := d.handle(proto.Request{Cmd: proto.CmdHostLook, Payload: b})
		return s
	}
	beat("h")
	d.drawPage()
	blank := keyColour(d, 4)

	// a label
	if s := look(proto.HostLook{Host: "h", Name: "media.now-playing", Rev: "1", Label: "Song\nBand"}); s != proto.StatusOK {
		t.Fatalf("label look: %d", s)
	}
	if l, ok := d.pc.look("media.now-playing"); !ok || l.label != "Song\nBand" {
		t.Errorf("look %+v", l)
	}
	if r := beat("h"); r.Looks["media.now-playing"] != "1" {
		t.Errorf("heartbeat reply %+v", r)
	}

	// an image, in parts: it fills the key
	img := image.NewRGBA(image.Rect(0, 0, 196, 196))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0x10, 0xa0, 0x30, 0xff
		if i%1000 == 0 { // some noise, so it spans parts
			img.Pix[i] = byte(i)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	if s := look(proto.HostLook{Host: "h", Name: "media.now-playing", Rev: "2", ImageSize: len(data), ImageSHA256: hex.EncodeToString(sum[:])}); s != proto.StatusOK {
		t.Fatalf("image look: %d", s)
	}
	for off := 0; off < len(data); off += proto.MaxConfigData {
		part := binary.LittleEndian.AppendUint32(nil, uint32(off))
		part = append(part, data[off:min(off+proto.MaxConfigData, len(data))]...)
		if s, _ := d.handle(proto.Request{Cmd: proto.CmdHostLookData, Payload: part}); s != proto.StatusOK {
			t.Fatalf("image part at %d: %d", off, s)
		}
	}
	d.drawPage()
	if got := keyColour(d, 4); got != 0x10a030 || got == blank {
		t.Errorf("image not drawn: %#x", got)
	}

	// a damaged image, and parts out of order
	look(proto.HostLook{Host: "h", Name: "x", Rev: "3", ImageSize: 4, ImageSHA256: "00"})
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdHostLookData, Payload: []byte{1, 0, 0, 0, 'a'}}); s != proto.StatusBadRequest {
		t.Error("part at the wrong offset taken")
	}
	look(proto.HostLook{Host: "h", Name: "x", Rev: "3", ImageSize: 4, ImageSHA256: "00"})
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdHostLookData, Payload: []byte{0, 0, 0, 0, 'a', 'b', 'c', 'd'}}); s != proto.StatusBadRequest {
		t.Error("an image with the wrong hash taken")
	}

	// limits, and another host's looks
	for i := range proto.MaxHostLooks + 1 {
		s := look(proto.HostLook{Host: "h", Name: fmt.Sprint("n", i), Rev: "1", Label: "x"})
		if (i < proto.MaxHostLooks-1) != (s == proto.StatusOK) {
			t.Errorf("look %d: status %d", i, s)
		}
	}
	if s := look(proto.HostLook{Host: "h", Name: "y", Label: "a\nb\nc\nd"}); s != proto.StatusBadRequest {
		t.Error("a 4-line label taken")
	}
	look(proto.HostLook{Host: "other", Name: "z", Rev: "1", Label: "x"})
	if _, ok := d.pc.look("z"); ok {
		t.Error("another host's look taken")
	}
	// clearing, and a new host starting with none
	look(proto.HostLook{Host: "h", Name: "n0"})
	if _, ok := d.pc.look("n0"); ok {
		t.Error("look not cleared")
	}
	if r := beat("h2"); len(r.Looks) != 0 {
		t.Errorf("a new host has looks: %v", r.Looks)
	}
}

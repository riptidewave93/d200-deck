package deckd

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/render"
)

func TestHandle(t *testing.T) {
	d := newTestDeckd()
	status, b := d.handle(proto.Request{Cmd: proto.CmdHello})
	var h proto.Hello
	if status != proto.StatusOK || json.Unmarshal(b, &h) != nil || h.Protocol != proto.Version || h.Keys != deck.KeyCount ||
		h.Features == nil || !slices.Contains(h.Features.Actions, "macro") {
		t.Errorf("hello: %d %s", status, b)
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdIdentify, Payload: []byte{deck.KeyCount}}); s != proto.StatusBadRequest {
		t.Errorf("identify of a missing key: %d", s)
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdIdentify, Payload: []byte{4}}); s != proto.StatusOK {
		t.Errorf("identify: %d", s)
	}
	runUI(t, d)
	if got := keyColour(d, 4); got != render.Colours[render.Identify] {
		t.Errorf("identify colour %#x", got)
	}

	// no key: the whole screen, white, for a second
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdIdentify}); s != proto.StatusOK {
		t.Errorf("identify the screen: %d", s)
	}
	runUI(t, d)
	d.drawKey(4, render.Pressed) // not drawn over it
	if keyColour(d, 4) != 0xffffff || keyColour(d, 13) != 0xffffff || d.canvas.Pix[len(d.canvas.Pix)-1] != 0xffffff {
		t.Error("screen not white")
	}
	runUI(t, d) // a second later: the keys again
	if got := keyColour(d, 4); got != render.Colours[render.Pressed] {
		t.Errorf("after the flash %#x", got)
	}

	if s, _ := d.handle(proto.Request{Cmd: proto.CmdActionHost}); s != proto.StatusBadRequest {
		t.Error("heartbeat without a host ID taken")
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdActionHost, Payload: []byte(`{"id":"h"}`)}); s != proto.StatusOK {
		t.Error("action-host heartbeat not taken")
	}
	if h, ok := d.pc.hostAlive(); !ok || h != "h" {
		t.Error("action host not set")
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdState, Payload: []byte("{")}); s != proto.StatusBadRequest {
		t.Errorf("bad state: %d", s)
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdState, Payload: []byte(`{"host":"h","state":{"discord.mute":true}}`)}); s != proto.StatusOK || !d.pc.getState("discord.mute") {
		t.Errorf("state not set: %d", s)
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdResult, Payload: []byte("{")}); s != proto.StatusBadRequest {
		t.Errorf("bad result: %d", s)
	}
	if s, _ := d.handle(proto.Request{Cmd: 0x7f}); s != proto.StatusUnknownCmd {
		t.Errorf("unknown command: %d", s)
	}
}

func TestChannelSend(t *testing.T) {
	var c channel
	if c.send(1, 0, nil) == nil {
		t.Error("sent on a closed channel")
	}
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	c.set(raw)
	defer c.set(nil)
	if err := c.send(5, proto.StatusOK, bytes.Repeat([]byte("x"), proto.MaxReply+100)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4*proto.RawInSize)
	n, _ := io.ReadAtLeast(r, buf, 2*proto.RawInSize)
	if n != 2*proto.RawInSize {
		t.Errorf("%d bytes written, want 2 reports", n)
	}
}

func TestKeyEvents(t *testing.T) {
	d := newTestDeckd()
	d.sendKey(3, true) // no queue: dropped, doesn't block
	d.keys = make(chan proto.KeyEvent, 1)
	d.sendKey(3, true)
	d.sendKey(3, false) // queue full: dropped
	if k := <-d.keys; k.Key != 3 || k.Release {
		t.Errorf("%+v", k)
	}
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	d.ch.set(raw)
	go d.sendKeys()
	d.sendKey(5, false)
	buf := make([]byte, proto.RawInSize)
	deadline := time.Now().Add(2 * time.Second)
	for {
		n, _ := r.Read(buf)
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no key event")
		}
		time.Sleep(5 * time.Millisecond)
	}
	part, err := proto.DecodeReply(buf)
	if err != nil || part.Seq != proto.EventSeq || part.Status != proto.EventKey || string(part.Payload) != `{"key":5,"release":true}` {
		t.Errorf("%+v %v", part, err)
	}
	close(d.keys)
}

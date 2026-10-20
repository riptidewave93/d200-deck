package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/proto/prototest"
)

// lookDevice is a fake deckd that keeps host looks and can forget them, as
// a restarted one does.
type lookDevice struct {
	*prototest.Device
	mu    sync.Mutex
	looks map[string]string // name to rev
	sizes map[string]int    // image bytes received
	pend  proto.HostLook
	got   int
	old   bool // an older deckd: no looks
}

func newLookDevice() *lookDevice {
	f := &lookDevice{Device: prototest.New(), looks: map[string]string{}, sizes: map[string]int{}}
	go f.Serve(func(r proto.Request) (byte, []byte) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Cmd {
		case proto.CmdHello:
			b, _ := json.Marshal(proto.Hello{Protocol: proto.Version, Serial: "00112233445566aa", Keys: 14})
			return proto.StatusOK, b
		case proto.CmdActionHost:
			if f.old {
				return proto.StatusOK, nil
			}
			b, _ := json.Marshal(proto.ActionHostReply{Looks: f.looks})
			return proto.StatusOK, b
		case proto.CmdHostLook:
			var l proto.HostLook
			json.Unmarshal(r.Payload, &l)
			switch {
			case l.ImageSize > 0:
				f.pend, f.got = l, 0
			case l.Label == "":
				delete(f.looks, l.Name)
			default:
				f.looks[l.Name] = l.Rev
			}
			return proto.StatusOK, nil
		case proto.CmdHostLookData:
			if f.got += len(r.Payload) - 4; f.got == f.pend.ImageSize {
				f.looks[f.pend.Name], f.sizes[f.pend.Name] = f.pend.Rev, f.got
			}
			return proto.StatusOK, nil
		case proto.CmdTime, proto.CmdState:
			return proto.StatusOK, nil
		}
		return proto.StatusUnknownCmd, nil
	})
	return f
}

func (f *lookDevice) has(name string) (rev string, size int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.looks[name], f.sizes[name]
}

func TestLooksSent(t *testing.T) {
	heartbeatPeriod = 5e6 // 5 ms
	t.Cleanup(func() { heartbeatPeriod = proto.ActionHostPeriod })
	dev := newLookDevice()
	defer dev.Close()
	d := newDaemon()
	d.find = func() []string { return []string{"/dev/hidraw2"} }
	d.open = func(string) (proto.Transport, error) { return dev, nil }
	image := make([]byte, 3*proto.MaxConfigData+10)
	d.setLook("media.now-playing", "Song\nBand", image)
	d.scan()
	waitFor(t, "look sent", func() bool { _, n := dev.has("media.now-playing"); return n == len(image) })
	rev, _ := dev.has("media.now-playing")

	d.setLook("media.now-playing", "Song\nBand", image) // the same: nothing to send
	if d.looks["media.now-playing"].rev != rev {
		t.Error("rev changed for the same look")
	}
	d.setLook("other", "Hi", nil)
	waitFor(t, "second look", func() bool { r, _ := dev.has("other"); return r != "" })
	d.setLook("other", "", nil)
	waitFor(t, "look cleared", func() bool { r, _ := dev.has("other"); return r == "" })

	// a restarted deckd has none: the next heartbeat brings it back
	dev.mu.Lock()
	clear(dev.looks)
	clear(dev.sizes)
	dev.mu.Unlock()
	waitFor(t, "look resent", func() bool { r, _ := dev.has("media.now-playing"); return r == rev })
}

func TestLooksOlderDeckd(t *testing.T) {
	// an older deckd's heartbeat reply is empty: deckhost sends no looks,
	// until deckd is updated while the PC stays connected
	heartbeatPeriod = 5e6
	t.Cleanup(func() { heartbeatPeriod = proto.ActionHostPeriod })
	dev := newLookDevice()
	dev.old = true
	defer dev.Close()
	d := newDaemon()
	d.find = func() []string { return []string{"/dev/hidraw3"} }
	d.open = func(string) (proto.Transport, error) { return dev, nil }
	d.setLook("media.now-playing", "Song", nil)
	d.scan()
	time.Sleep(50 * time.Millisecond)
	if r, _ := dev.has("media.now-playing"); r != "" {
		t.Fatal("look sent to an older deckd")
	}
	dev.mu.Lock()
	dev.old = false
	dev.mu.Unlock()
	waitFor(t, "look after the update", func() bool { r, _ := dev.has("media.now-playing"); return r != "" })
}

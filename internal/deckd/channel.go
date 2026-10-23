package deckd

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/serial"
	"github.com/riptidewave93/d200-deck/proto"
)

// Version is deckd's version, set by its main.
var Version = "dev"

// channel is the config channel gadget, shared by the request loop and
// events. send writes a whole message under the lock, so the reports of two
// messages never interleave.
type channel struct {
	mu  sync.Mutex
	raw *hidg.Raw // nil while closed
}

func (c *channel) send(seq uint16, status byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.raw == nil {
		return errors.New("config channel closed")
	}
	for _, r := range proto.EncodeReply(seq, status, payload) {
		if err := c.raw.Write(r); err != nil {
			return err
		}
	}
	return nil
}

func (c *channel) set(raw *hidg.Raw) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.raw != nil {
		c.raw.Close()
	}
	c.raw = raw
}

// serveChannel answers config channel requests until the process exits,
// reopening the gadget if it goes away (e.g. the USB gadget was rebound).
func (d *deckd) serveChannel(path string) {
	buf := make([]byte, 4096)
	// log each error once, not on every retry while no PC is listening
	var last string
	logErr := func(err error) {
		if s := err.Error(); s != last {
			log.Printf("config channel: %v", err)
			last = s
		}
	}
	for {
		raw, err := hidg.OpenRaw(path)
		if err != nil {
			logErr(err)
			time.Sleep(2 * time.Second)
			continue
		}
		d.ch.set(raw)
		for {
			n, err := raw.Read(buf)
			if err != nil {
				logErr(err)
				break
			}
			if last != "" {
				log.Print("config channel: ok")
				last = ""
			}
			req, err := proto.DecodeRequest(buf[:n])
			if err == nil && req.Seq == proto.EventSeq {
				continue // its reply would look like an event
			}
			status, reply := byte(proto.StatusBadRequest), []byte(nil)
			if err == nil {
				status, reply = d.handle(req)
			}
			if status == statusLater {
				continue
			}
			if err := d.ch.send(req.Seq, status, reply); err != nil {
				logErr(err)
			}
		}
		d.ch.set(nil)
		time.Sleep(time.Second)
	}
}

// features are what this deckd supports, for hello.
var features = deck.Features()

func (d *deckd) handle(req proto.Request) (status byte, reply []byte) {
	switch req.Cmd {
	case proto.CmdHello:
		info, _ := osrelease.Read(osrelease.Path)
		b, _ := json.Marshal(proto.Hello{Protocol: proto.Version, Firmware: Version, OS: info, Serial: readSerial(),
			Keys: deck.KeyCount, Model: model(), IPv4: usbIPv4(), Features: &features})
		return proto.StatusOK, b
	case proto.CmdIdentify:
		if len(req.Payload) == 0 {
			d.ui <- d.flashAll
			return proto.StatusOK, nil
		}
		if len(req.Payload) != 1 || int(req.Payload[0]) >= deck.KeyCount {
			return proto.StatusBadRequest, nil
		}
		i := int(req.Payload[0])
		d.ui <- func() { d.flash(i) }
		return proto.StatusOK, nil
	case proto.CmdActionHost:
		var h proto.ActionHost
		if json.Unmarshal(req.Payload, &h) != nil || h.ID == "" {
			return proto.StatusBadRequest, nil
		}
		d.pc.heartbeat(h)
		if evs := d.pc.unsentReleases(); len(evs) > 0 {
			go d.sendReleases(evs)
		}
		return okJSON(proto.ActionHostReply{Looks: d.pc.lookRevs()})
	case proto.CmdHostLook:
		var l proto.HostLook
		if json.Unmarshal(req.Payload, &l) != nil || d.pc.setLook(l) != nil {
			return proto.StatusBadRequest, nil
		}
		return proto.StatusOK, nil
	case proto.CmdHostLookData:
		if len(req.Payload) < 4 || d.pc.lookData(int(binary.LittleEndian.Uint32(req.Payload)), req.Payload[4:]) != nil {
			return proto.StatusBadRequest, nil
		}
		return proto.StatusOK, nil
	case proto.CmdState:
		var s proto.State
		if json.Unmarshal(req.Payload, &s) != nil {
			return proto.StatusBadRequest, nil
		}
		d.pc.setState(s)
		return proto.StatusOK, nil
	case proto.CmdConfigInfo, proto.CmdConfigRead, proto.CmdConfigWrite, proto.CmdConfigData, proto.CmdConfigCommit:
		return d.configRequest(req)
	case proto.CmdHost:
		return okJSON(d.pc.hostInfo())
	case proto.CmdTime:
		var t proto.Time
		if json.Unmarshal(req.Payload, &t) != nil || t.UnixMS <= 0 {
			return proto.StatusBadRequest, nil
		}
		if err := d.clock.set(time.UnixMilli(t.UnixMS)); err != nil {
			return failed(err)
		}
		return proto.StatusOK, nil
	case proto.CmdSettings:
		return d.settings(req.Seq, req.Payload)
	case proto.CmdReset:
		return d.reset(req.Seq)
	case proto.CmdResult:
		var r proto.Result
		if json.Unmarshal(req.Payload, &r) != nil {
			return proto.StatusBadRequest, nil
		}
		d.pc.result(r)
		return proto.StatusOK, nil
	}
	return proto.StatusUnknownCmd, nil
}

// compatiblePath lists the device's compatible strings, NUL-separated.
var compatiblePath = "/proc/device-tree/compatible"

func model() string {
	b, _ := os.ReadFile(compatiblePath)
	m, _, _ := strings.Cut(string(b), "\x00")
	return m
}

func readSerial() string {
	s, _ := serial.Read()
	return s
}

// sendKey tells the config channel's clients about a key, without making
// the keys wait for them: with no PC reading, each report takes up to 50 ms
// to give up on. If too many are queued, it's dropped.
func (d *deckd) sendKey(key int, pressed bool) {
	select {
	case d.keys <- proto.KeyEvent{Key: key, Release: !pressed}:
	default:
	}
}

func (d *deckd) sendKeys() {
	for k := range d.keys {
		b, _ := json.Marshal(k)
		d.ch.send(proto.EventSeq, proto.EventKey, b) // nobody may be listening
	}
}

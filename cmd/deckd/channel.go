package main

import (
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/serial"
	"github.com/riptidewave93/d200-deck/proto"
)

// version is set at build time.
var version = "dev"

// channel is the config channel gadget, shared by the request loop and
// events. send writes a whole message under the lock, so the reports of two
// messages never interleave.
type channel struct {
	mu  sync.Mutex
	raw *hidg.Raw // nil while closed
}

func (c *channel) send(seq, status byte, payload []byte) error {
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
			status, reply := byte(proto.StatusBadRequest), []byte(nil)
			if err == nil {
				status, reply = d.handle(req)
			}
			if err := d.ch.send(req.Seq, status, reply); err != nil {
				logErr(err)
			}
		}
		d.ch.set(nil)
		time.Sleep(time.Second)
	}
}

func (d *deckd) handle(req proto.Request) (status byte, reply []byte) {
	switch req.Cmd {
	case proto.CmdHello:
		info, _ := osrelease.Read(osrelease.Path)
		b, _ := json.Marshal(proto.Hello{Protocol: proto.Version, Firmware: version, OS: info, Serial: readSerial(), Keys: deck.KeyCount})
		return proto.StatusOK, b
	case proto.CmdIdentify:
		if len(req.Payload) != 1 || int(req.Payload[0]) >= deck.KeyCount {
			return proto.StatusBadRequest, nil
		}
		i := int(req.Payload[0])
		d.ui <- func() { d.flash(i) }
		return proto.StatusOK, nil
	case proto.CmdActionHost:
		d.pc.heartbeat()
		return proto.StatusOK, nil
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

func readSerial() string {
	s, _ := serial.Read()
	return s
}

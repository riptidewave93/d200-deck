package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/proto"
)

// version is set at build time.
var version = "dev"

// serveChannel answers config channel requests until the process exits,
// reopening the gadget if it goes away (e.g. the USB gadget was rebound).
func (d *deckd) serveChannel(path string) {
	buf := make([]byte, 4096)
	for {
		raw, err := hidg.OpenRaw(path)
		if err != nil {
			log.Printf("config channel: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		for {
			n, err := raw.Read(buf)
			if err != nil {
				log.Printf("config channel: %v", err)
				break
			}
			req, err := proto.DecodeRequest(buf[:n])
			status, reply := byte(proto.StatusBadRequest), []byte(nil)
			if err == nil {
				status, reply = d.handle(req)
			}
			for _, r := range proto.EncodeReply(req.Seq, status, reply) {
				if err := raw.Write(r); err != nil {
					log.Printf("config channel: %v", err)
					break
				}
			}
		}
		raw.Close()
		time.Sleep(time.Second)
	}
}

func (d *deckd) handle(req proto.Request) (status byte, reply []byte) {
	switch req.Cmd {
	case proto.CmdHello:
		b, _ := json.Marshal(proto.Hello{Protocol: proto.Version, Firmware: version, Serial: serial(), Keys: deck.KeyCount})
		return proto.StatusOK, b
	case proto.CmdIdentify:
		if len(req.Payload) != 1 || int(req.Payload[0]) >= deck.KeyCount {
			return proto.StatusBadRequest, nil
		}
		i := int(req.Payload[0])
		d.ui <- func() { d.flash(i) }
		return proto.StatusOK, nil
	}
	return proto.StatusUnknownCmd, nil
}

func serial() string {
	b, _ := os.ReadFile("/proc/device-tree/serial-number")
	return strings.TrimRight(string(b), "\x00\n")
}

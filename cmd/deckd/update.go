package main

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/ota"
)

// overlay is a screen shown instead of the config's pages, such as the
// update screen.
type overlay struct {
	keys  map[int]deck.Button
	press func(key int) // nil: presses do nothing
}

func (d *deckd) setOverlay(o *overlay) {
	d.overlay = o
	d.drawPage()
}

// confirmTimeout is how long the confirm screen waits for a key.
var confirmTimeout = 2 * time.Minute

const (
	red   = "#c62828"
	green = "#2e7d32"
	amber = "#f9a825"
	white = "#ffffff"
)

// confirmScreen asks whether to install c; answer gets the choice.
func confirmScreen(c ota.ConfirmRequest, answer func(bool)) *overlay {
	keys := map[int]deck.Button{
		2:  {Icon: "system_update_alt", Label: "Update?"},
		7:  {Label: c.Version + "\n" + c.Flavour},
		10: {Icon: "close", Label: "Cancel", Color: white, Background: red},
		13: {Icon: "system_update_alt", Label: "Install", Color: white, Background: green},
	}
	if c.Older {
		keys[8] = deck.Button{Icon: "warning", Label: "Not newer", Background: amber, Color: "#000000"}
	}
	return &overlay{keys: keys, press: func(key int) {
		switch key {
		case 10:
			answer(false)
		case 13:
			answer(true)
		}
	}}
}

func progressScreen(step string) *overlay {
	return &overlay{keys: map[int]deck.Button{
		2:  {Icon: "system_update_alt", Label: "Updating"},
		7:  {Label: step},
		13: {Icon: "warning", Label: "Don't unplug", Color: "#000000", Background: amber},
	}}
}

// serveUpdates shows deck-update's requests, one connection at a time.
func (d *deckd) serveUpdates(path string) {
	os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("update screen: %v", err)
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		log.Printf("update screen: %v", err)
		l.Close()
		return
	}
	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("update screen: %v", err)
			return
		}
		d.updateSession(c)
	}
}

func (d *deckd) updateSession(c net.Conn) {
	defer c.Close()
	defer func() { d.ui <- func() { d.setOverlay(nil) } }()
	dec, enc := json.NewDecoder(c), json.NewEncoder(c)
	for {
		var req ota.UIRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		switch {
		case req.Confirm != nil:
			answer := make(chan bool, 1)
			screen := confirmScreen(*req.Confirm, func(ok bool) {
				select {
				case answer <- ok:
				default:
				}
			})
			d.ui <- func() { d.setOverlay(screen) }
			ok := false
			select {
			case ok = <-answer:
			case <-time.After(confirmTimeout):
			}
			log.Printf("update %s: install %v", req.Confirm.Version, ok)
			if enc.Encode(ota.UIReply{OK: ok}) != nil || !ok {
				return
			}
		case req.Progress != "":
			screen := progressScreen(req.Progress)
			d.ui <- func() { d.setOverlay(screen) }
		}
	}
}

// writeReady tells deck-update deckd is up.
func writeReady(path string) {
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		log.Print(err)
	}
}

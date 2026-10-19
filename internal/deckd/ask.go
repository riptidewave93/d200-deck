package deckd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/proto"
)

// statusLater marks a request answered later, by ask.
const statusLater = 0xff

// pressDelay is how long a new screen ignores presses, so a press meant for
// what was there before doesn't answer it.
var pressDelay = time.Second

var (
	errAsking    = errors.New("the device is already asking about another change")
	errBusy      = errors.New("the device is busy; try again")
	errCancelled = errors.New("cancelled on the device")
	errNoAnswer  = errors.New("not confirmed on the device in time")
)

// ask asks on the device before a change a config channel client asked
// for: on Save, it runs then and sends its reply to seq; otherwise it sends
// why not. It returns statusLater, or a failure if it can't ask.
func (d *deckd) ask(seq uint16, title, detail, ok string, then func() (byte, []byte)) (byte, []byte) {
	if !d.asking.CompareAndSwap(false, true) {
		return failed(errAsking)
	}
	go func() {
		defer d.asking.Store(false)
		answer := make(chan error, 1)
		reply := func(err error) {
			select {
			case answer <- err:
			default:
			}
		}
		o := askScreen(title, detail, ok, func(yes bool) {
			if yes {
				reply(nil)
			} else {
				reply(errCancelled)
			}
		})
		d.ui <- func() {
			if d.overlay != nil {
				reply(errBusy)
				return
			}
			d.setOverlay(o)
		}
		var err error
		select {
		case err = <-answer:
		case <-time.After(proto.ConfirmTimeout):
			err = errNoAnswer
		}
		d.ui <- func() {
			if d.overlay == o {
				d.setOverlay(nil)
			}
		}
		var status byte
		var b []byte
		if err != nil {
			status, b = failed(err)
		} else {
			status, b = then()
		}
		if d.reply != nil {
			d.reply(seq, status, b)
		} else if err := d.ch.send(seq, status, b); err != nil {
			log.Printf("config channel: %v", err)
		}
	}()
	return statusLater, nil
}

// askScreen asks whether to make a change.
//
//	. . title  . .
//	. . detail . .
//	Cancel . . [  ok  ]
func askScreen(title, detail, ok string, answer func(bool)) *overlay {
	return &overlay{
		keys: map[int]deck.Button{
			2:  {Icon: "help", Label: title},
			7:  {Label: detail},
			10: {Icon: "close", Label: "Cancel", Color: white, Background: red},
		},
		drawn: map[int]func(*gfx.Canvas, gfx.Rect){
			13: text("go-bold", greenRGB, 0xffffff, line{ok, 56}, line{"Only if you made this change", 26}),
		},
		press: func(key int) {
			switch key {
			case 10:
				answer(false)
			case 13:
				answer(true)
			}
		},
	}
}

// newActions counts the keys of c that do something (more than move between
// pages) that they didn't in old.
func newActions(old, c *deck.Config) int {
	n := 0
	for name, p := range c.Pages {
		for i, b := range p.Keys {
			switch b.Action.Type {
			case "", deck.ActionPage, deck.ActionBack, deck.ActionHome:
				continue
			}
			was, _ := json.Marshal(old.Pages[name].Keys[i].Action)
			now, _ := json.Marshal(b.Action)
			if string(was) != string(now) {
				n++
			}
		}
	}
	return n
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

package deckd

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/proto"
)

// resetMarker asks the image's boot to erase /data before deckd starts.
var resetMarker = "/data/.reset"

// reboot restarts the device; resetDelay lets the reply reach the PC first.
var (
	reboot     = func() error { return exec.Command("reboot").Run() }
	resetDelay = time.Second
)

const redRGB = 0xc62828

// reset erases the config and settings and restarts, once confirmed on
// the device.
func (d *deckd) reset(seq uint16) (byte, []byte) {
	return d.askOn(seq, resetScreen, func() (byte, []byte) {
		if err := markReset(resetMarker); err != nil {
			log.Printf("reset: %v", err)
			return failed(err)
		}
		log.Print("reset: restarting to erase /data")
		d.restart()
		return proto.StatusOK, nil
	})
}

// markReset writes the marker and makes sure it's on flash.
func markReset(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// resetScreen asks whether to erase everything.
//
//	. . Erase all? . .
//	. . detail     . .
//	Cancel . . [ Erase ]
func resetScreen(answer func(bool)) *overlay {
	return &overlay{
		keys: map[int]deck.Button{
			2:  {Icon: "help", Label: "Erase all?"},
			7:  {Label: "Keys and\nsettings"},
			10: {Icon: "close", Label: "Cancel"},
		},
		drawn: map[int]func(*gfx.Canvas, gfx.Rect){
			13: text("go-bold", redRGB, 0xffffff, line{"Erase", 56}, line{"Then the device restarts", 26}),
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

func restartingScreen() *overlay {
	return &overlay{keys: map[int]deck.Button{7: {Label: "Restarting"}}}
}

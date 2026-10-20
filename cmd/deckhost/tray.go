package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"path/filepath"
	"time"

	"fyne.io/systray"

	"github.com/riptidewave93/d200-deck/deck"
)

func runTray() {
	ensureDaemon()
	systray.Run(trayReady, nil)
}

// ensureDaemon starts the daemon if nothing has: on Windows, or on Linux
// without systemd or a session that starts its unit.
func ensureDaemon() {
	if _, running := daemonStatus(); !running {
		if err := restartService(); err != nil {
			log.Printf("starting the daemon: %v", err)
		}
	}
}

func trayReady() {
	icons := map[bool][]byte{true: trayIcon(drawIcon(0x2f6fde)), false: trayIcon(drawIcon(0x8a8a8a))}
	systray.SetIcon(icons[false])
	systray.SetTitle("D200")
	systray.SetTooltip("D200 Deck")

	status := systray.AddMenuItem("D200: checking…", "")
	status.Disable()
	var lines []*systray.MenuItem
	for range maxTrayDevices + maxTrayApps {
		l := systray.AddMenuItem("", "")
		l.Disable()
		l.Hide()
		lines = append(lines, l)
	}
	systray.AddSeparator()
	configure := systray.AddMenuItem("Configure", "Open the configuration website")
	// One D200 gets a plain Update item; several get a submenu, one per device.
	update := systray.AddMenuItem("Update", "Open the device's update page")
	updateMenu := systray.AddMenuItem("Update", "Open a device's update page")
	updateMenu.Hide()
	picked := make(chan int)
	var updateItems []*systray.MenuItem
	for i := range maxTrayDevices {
		it := updateMenu.AddSubMenuItem("", "")
		it.Hide()
		updateItems = append(updateItems, it)
		go func() {
			for range it.ClickedCh {
				picked <- i
			}
		}()
	}
	var serials []string
	osUpdate := systray.AddMenuItem("", "Open the release page to download it")
	osUpdate.Hide()
	var osURL string
	systray.AddSeparator()
	restart := systray.AddMenuItem("Restart", "Restart deckhost")
	exit := systray.AddMenuItem("Exit", "Stop deckhost and close this icon")

	refresh := func() {
		s, running := daemonStatus()
		header, text := describe(s, running)
		status.SetTitle(header)
		for i, l := range lines {
			if i < len(text) {
				l.SetTitle(text[i])
				l.Show()
			} else {
				l.Hide()
			}
		}
		serials = updateSerials(s)
		systray.SetIcon(icons[len(serials) > 0])
		if len(serials) > 0 {
			update.Enable()
		} else {
			update.Disable()
		}
		if len(serials) > 1 {
			update.Hide()
			updateMenu.Show()
		} else {
			update.Show()
			updateMenu.Hide()
		}
		for i, it := range updateItems {
			if i < len(serials) {
				it.SetTitle(shortSerial(serials[i]))
				it.Show()
			} else {
				it.Hide()
			}
		}
		osURL = ""
		for _, d := range s.Connected() {
			if d.OSUpdate != nil {
				osUpdate.SetTitle("Download OS " + d.OSUpdate.Version)
				osURL = d.OSUpdate.URL
			}
		}
		if osURL != "" {
			osUpdate.Show()
		} else {
			osUpdate.Hide()
		}
	}
	refresh()

	go func() {
		tick := time.NewTicker(3 * time.Second)
		for {
			select {
			case <-tick.C:
				refresh()
			case <-configure.ClickedCh:
				ask("configure", func() error { return openURL(deck.ConfigureURL) })
			case <-update.ClickedCh:
				ask("update", notRunning)
			case i := <-picked:
				if i < len(serials) {
					ask("update "+serials[i], notRunning)
				}
			case <-osUpdate.ClickedCh:
				if err := openURL(osURL); osURL != "" && err != nil {
					log.Printf("release page: %v", err)
				}
			case <-restart.ClickedCh:
				ask("restart", restartService)
				time.Sleep(time.Second)
				refresh()
			case <-exit.ClickedCh:
				control("exit")
				systray.Quit()
				return
			}
		}
	}()
}

// updateSerials are the serials of the connected devices the menu offers updates for.
func updateSerials(s Status) []string {
	var serials []string
	for _, d := range s.Connected() {
		if len(serials) < maxTrayDevices {
			serials = append(serials, d.Hello.Serial)
		}
	}
	return serials
}

// notRunning is the fallback for actions only the daemon can do.
func notRunning() error { return errors.New("deckhost isn't running") }

// maxTrayDevices is how many devices the menu lists.
const maxTrayDevices = 8

// maxTrayApps is how many app integrations it lists.
const maxTrayApps = 4

// describe is the menu's status header and one line per device.
func describe(s Status, running bool) (header string, lines []string) {
	if !running {
		return "deckhost isn't running", nil
	}
	n := len(s.Connected())
	switch {
	case n == 0:
		header = "D200 not connected"
	case n == 1:
		header = "D200 connected"
	default:
		header = fmt.Sprintf("%d D200s connected", n)
	}
	for _, d := range s.Devices {
		var l string
		switch {
		case d.Hello != nil && len(s.Devices) == 1:
			l = fmt.Sprintf("OS %s (%s)", d.Hello.OS.Version, d.Hello.OS.Flavour)
		case d.Hello != nil:
			l = fmt.Sprintf("%s: OS %s (%s)", shortSerial(d.Hello.Serial), d.Hello.OS.Version, d.Hello.OS.Flavour)
		case d.Error != "":
			l = filepath.Base(d.Path) + ": " + d.Error
		default:
			continue
		}
		if len(l) > 80 {
			l = l[:77] + "..."
		}
		lines = append(lines, l)
	}
	if len(lines) > maxTrayDevices {
		lines = lines[:maxTrayDevices]
	}
	for i, a := range s.Apps {
		if i == maxTrayApps {
			break
		}
		lines = append(lines, a.Title+": "+shorten(a.Status, 70))
	}
	return header, lines
}

// shortSerial names a device by the end of its serial.
func shortSerial(s string) string {
	if len(s) > 6 {
		s = s[len(s)-6:]
	}
	return "D200 …" + s
}

// ask passes cmd to the daemon, or runs fallback if the daemon isn't running.
func ask(cmd string, fallback func() error) {
	_, err := control(cmd)
	if err == nil {
		return
	}
	if _, running := daemonStatus(); running {
		log.Printf("%s: %v", cmd, err)
		return
	}
	if err := fallback(); err != nil {
		log.Printf("%s: %v", cmd, err)
	}
}

func daemonStatus() (s Status, running bool) {
	reply, err := control("status")
	if err != nil {
		return Status{}, false
	}
	json.Unmarshal([]byte(reply), &s)
	return s, true
}

// drawIcon draws the tray icon: a deck of 3x2 keys on a body of colour body.
func drawIcon(body uint32) []byte {
	const size, ss = 64, 4 // pixels, samples per pixel per axis
	bodyRGBA := color.RGBA{uint8(body >> 16), uint8(body >> 8), uint8(body), 0xff}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	type rect struct{ x0, y0, x1, y1, r float64 }
	shapes := []rect{{2, 10, 62, 54, 9}}
	for row := 0; row < 2; row++ {
		for col := 0; col < 3; col++ {
			x, y := 9+float64(col)*16, 17+float64(row)*16
			shapes = append(shapes, rect{x, y, x + 14, y + 14, 3})
		}
	}
	inside := func(s rect, x, y float64) bool {
		cx := min(max(x, s.x0+s.r), s.x1-s.r)
		cy := min(max(y, s.y0+s.r), s.y1-s.r)
		return x >= s.x0 && x < s.x1 && y >= s.y0 && y < s.y1 && (x-cx)*(x-cx)+(y-cy)*(y-cy) <= s.r*s.r
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var nBody, nKey int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x, y := float64(px)+(float64(sx)+0.5)/ss, float64(py)+(float64(sy)+0.5)/ss
					switch {
					case !inside(shapes[0], x, y):
					case func() bool {
						for _, k := range shapes[1:] {
							if inside(k, x, y) {
								return true
							}
						}
						return false
					}():
						nKey++
					default:
						nBody++
					}
				}
			}
			n := nBody + nKey
			if n == 0 {
				continue
			}
			// colour: mix of body and key samples; alpha: coverage
			mix := func(a, b uint8) uint8 { return uint8((int(a)*nBody + int(b)*nKey) / n) }
			a := n * 255 / (ss * ss)
			img.SetRGBA(px, py, color.RGBA{
				uint8(int(mix(bodyRGBA.R, white.R)) * a / 255),
				uint8(int(mix(bodyRGBA.G, white.G)) * a / 255),
				uint8(int(mix(bodyRGBA.B, white.B)) * a / 255),
				uint8(a),
			})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/systray"

	"github.com/riptidewave93/d200-deck/deck"
)

func runTray() {
	log.SetPrefix("tray: ")
	logToFile()
	ensureDaemon()
	// one tray: starting it again, e.g. from the app launcher, does nothing
	if path, err := socketPath(); err == nil {
		lock, err := lockInstance(path + ".tray.lock")
		if err != nil {
			log.Print("the tray is already running")
			return
		}
		defer lock.Close()
	}
	systray.Run(trayReady, nil)
}

// ensureDaemon starts the daemon if nothing has (on Windows and macOS, or on
// Linux without systemd or a session that starts its unit), or if the one
// running has lost its program file, so it couldn't restart: the app was
// moved, say.
func ensureDaemon() {
	s, running := daemonStatus()
	if running && programGone(s) {
		log.Printf("the daemon's %s is gone: starting this one", s.Exe)
		control("exit")
		for i := 0; i < 30 && running; i++ {
			time.Sleep(100 * time.Millisecond)
			_, running = daemonStatus()
		}
	}
	if !running {
		if err := restartService(); err != nil {
			exe, _ := os.Executable()
			log.Printf("starting the daemon (%s): %v", exe, err)
		}
	}
}

// programGone reports whether the daemon's program file is gone.
func programGone(s Status) bool {
	if s.Exe == "" {
		return false // an older daemon doesn't say
	}
	_, err := os.Stat(s.Exe)
	return err != nil
}

func trayReady() {
	icons := map[bool][]byte{true: trayIcon(drawIcon(iconBlue, trayIconSize, false)), false: trayIcon(drawIcon(0x8a8a8a, trayIconSize, false))}
	systray.SetIcon(icons[false])
	if runtime.GOOS != "darwin" { // macOS shows the title as text next to the icon
		systray.SetTitle("D200")
	}
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
	file := newDeviceMenu("Install an update file…", "Open the device's update page")
	install := newDeviceMenu("Update device", "Download the new device OS and send it to the device")
	hostUpdate := systray.AddMenuItem("", "Open the release page to download it")
	hostUpdate.Hide()
	var hostURL string
	systray.AddSeparator()
	checks := systray.AddMenuItemCheckbox("Check for updates", "Look for new device OS and deckhost releases", true)
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
		systray.SetIcon(icons[len(s.Connected()) > 0])
		file.set(fileEntries(s), true)
		install.set(installEntries(s), false)
		hostURL = ""
		if u := s.HostUpdate; u != nil {
			hostURL = u.URL
			hostUpdate.SetTitle(strings.TrimSpace("Download deckhost " + u.Version))
			hostUpdate.Show()
		} else {
			hostUpdate.Hide()
		}
		if !running {
			checks.Disable()
			return
		}
		checks.Enable()
		if s.CheckUpdates {
			checks.Check()
		} else {
			checks.Uncheck()
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
			case i := <-file.picked:
				ask(strings.TrimSpace("update "+file.serial(i)), notRunning)
			case i := <-install.picked:
				ask(strings.TrimSpace("install "+install.serial(i)), notRunning)
				refresh()
			case <-hostUpdate.ClickedCh:
				if err := openURL(hostURL); hostURL != "" && err != nil {
					log.Printf("release page: %v", err)
				}
			case <-checks.ClickedCh:
				ask(map[bool]string{true: "checks off", false: "checks on"}[checks.Checked()], notRunning)
				refresh()
			case <-restart.ClickedCh:
				ask("restart", restartService)
				time.Sleep(time.Second)
				ensureDaemon() // one that couldn't restart itself
				refresh()
			case <-exit.ClickedCh:
				control("exit")
				systray.Quit()
				return
			}
		}
	}()
}

// deviceMenu is a menu item for one device, which becomes a submenu of one
// item per device when there are several.
type deviceMenu struct {
	title     string // when there's no device
	one, many *systray.MenuItem
	items     []*systray.MenuItem
	picked    chan int    // the entry clicked
	entries   []menuEntry // what's shown, used on the menu's goroutine only
}

// menuEntry is one device's item in a deviceMenu.
type menuEntry struct {
	serial    string
	one, many string // its title as the only item, and in the submenu
	busy      bool   // greyed out
}

func newDeviceMenu(title, tooltip string) *deviceMenu {
	m := &deviceMenu{title: title, one: systray.AddMenuItem(title, tooltip), many: systray.AddMenuItem(title, tooltip),
		picked: make(chan int)}
	m.many.Hide()
	for i := range maxTrayDevices {
		it := m.many.AddSubMenuItem("", "")
		it.Hide()
		m.items = append(m.items, it)
		go func() {
			for range it.ClickedCh {
				m.picked <- i
			}
		}()
	}
	go func() {
		for range m.one.ClickedCh {
			m.picked <- 0
		}
	}()
	return m
}

// serial is the device of entry i.
func (m *deviceMenu) serial(i int) string {
	if i < len(m.entries) {
		return m.entries[i].serial
	}
	return ""
}

// set shows entries; with none, the item is greyed out if keepEmpty, else
// hidden.
func (m *deviceMenu) set(entries []menuEntry, keepEmpty bool) {
	m.entries = entries
	switch {
	case len(entries) > 1:
		m.one.Hide()
		m.many.Show()
	case len(entries) == 1:
		m.many.Hide()
		m.one.SetTitle(entries[0].one)
		enable(m.one, !entries[0].busy)
		m.one.Show()
	case keepEmpty:
		m.many.Hide()
		m.one.SetTitle(m.title)
		m.one.Disable()
		m.one.Show()
	default:
		m.one.Hide()
		m.many.Hide()
	}
	for i, it := range m.items {
		if len(entries) > 1 && i < len(entries) {
			it.SetTitle(entries[i].many)
			enable(it, !entries[i].busy)
			it.Show()
		} else {
			it.Hide()
		}
	}
}

func enable(it *systray.MenuItem, on bool) {
	if on {
		it.Enable()
	} else {
		it.Disable()
	}
}

// fileEntries are the devices "Install an update file…" opens the update
// page of.
func fileEntries(s Status) []menuEntry {
	var es []menuEntry
	for _, d := range s.Updatable() {
		if len(es) < maxTrayDevices {
			es = append(es, menuEntry{serial: d.Hello.Serial, one: "Install an update file…", many: shortSerial(d.Hello.Serial)})
		}
	}
	return es
}

// installEntries are the devices "Update device" can send a new OS to.
func installEntries(s Status) []menuEntry {
	var es []menuEntry
	for _, d := range s.Updatable() {
		if d.OSUpdate == nil || len(es) == maxTrayDevices {
			continue
		}
		name := shortSerial(d.Hello.Serial)
		e := menuEntry{serial: d.Hello.Serial, one: "Update device to " + d.OSUpdate.Version, many: name + " to " + d.OSUpdate.Version}
		if d.Install != "" {
			e = menuEntry{serial: d.Hello.Serial, one: d.Install, many: name + ": " + d.Install, busy: true}
		}
		es = append(es, e)
	}
	return es
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
		case d.Hello != nil && d.Error != "": // too old to talk to
			l = shortSerial(d.Hello.Serial) + ": " + d.Error
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

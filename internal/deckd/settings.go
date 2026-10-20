package deckd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/netid"
	"github.com/riptidewave93/d200-deck/proto"
)

// usbConf holds the image's USB gadget settings as KEY=value lines; the
// image reads it at boot (see d200-buildroot's docs/linux.md).
var usbConf = "/data/usb-gadget.conf"

// applyUSB makes the USB gadget match usbConf, re-plugging it if it changes.
var applyUSB = func() error {
	out, err := exec.Command("/etc/init.d/S30usb-gadget", "apply").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// rebindDelay lets the reply, then the event, reach the PC before the
// re-plug.
var rebindDelay = 250 * time.Millisecond

// confValue is the value of key's last line in path, as the image reads it.
func confValue(path, key string) string {
	b, _ := os.ReadFile(path)
	v := ""
	for _, l := range strings.Split(string(b), "\n") {
		if x, ok := strings.CutPrefix(strings.TrimSpace(l), key+"="); ok {
			v = x
		}
	}
	return v
}

// usbDrive reads the drive setting: on unless it's exactly 0.
func usbDrive(path string) bool { return confValue(path, "drive") != "0" }

// usbNet4 reads the address override; "" if there's none or the image would
// ignore it.
func usbNet4(path string) string {
	if v := confValue(path, "net4"); validNet4(v) {
		return v
	}
	return ""
}

// validNet4 checks an address as the image does: a dotted quad without
// leading zeros, its first part 1-223 and not 127.
func validNet4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || p != strconv.Itoa(n) || n < 0 || n > 255 {
			return false
		}
		if i == 0 && (n < 1 || n > 223 || n == 127) {
			return false
		}
	}
	return true
}

// setConf rewrites path with key=value, keeping its other lines; an empty
// value removes the key.
func setConf(path, key, value string) error {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l != "" && !strings.HasPrefix(strings.TrimSpace(l), key+"=") {
			lines = append(lines, l)
		}
	}
	if value != "" {
		lines = append(lines, key+"="+value)
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// settings answers CmdSettings.
// settings reads or changes the settings. A new USB network can cut the PC
// off from an address it uses, so the device asks first.
func (d *deckd) settings(seq uint16, payload []byte) (status byte, reply []byte) {
	if len(payload) > 0 {
		var s proto.Settings
		if json.Unmarshal(payload, &s) != nil {
			return proto.StatusBadRequest, nil
		}
		if s.USBNet4 != nil && *s.USBNet4 != "" && !validNet4(*s.USBNet4) {
			return proto.StatusFailed, []byte("usb_net4 must be an IPv4 address such as 10.77.0.4, its first part 1-223 and not 127")
		}
		if s.HostOS != nil && !validHostOS(*s.HostOS) {
			return proto.StatusFailed, []byte(`host_os must be "mac", "other" or ""`)
		}
		if s.HostOS != nil && *s.HostOS != d.host.get() {
			if _, err := d.host.set(*s.HostOS); err != nil {
				log.Printf("host os: %v", err)
			}
			log.Printf("settings: host_os=%s", *s.HostOS)
		}
		var changes [][2]string
		if s.USBDrive != nil && *s.USBDrive != usbDrive(usbConf) {
			changes = append(changes, [2]string{"drive", map[bool]string{true: "1", false: "0"}[*s.USBDrive]})
		}
		if s.USBNet4 != nil && *s.USBNet4 != usbNet4(usbConf) {
			changes = append(changes, [2]string{"net4", *s.USBNet4})
		}
		if s.Brightness != nil && d.light != nil {
			d.light.set(*s.Brightness)
		}
		if s.SleepScreen != nil && d.light != nil && *s.SleepScreen != d.sleep.get() {
			ok, err := d.sleep.set(*s.SleepScreen)
			if !ok {
				return proto.StatusFailed, []byte(`sleep_screen must be "zzz" or "off"`)
			}
			if err != nil {
				log.Printf("sleep screen: %v", err)
			}
			d.ui <- d.showSleep // if the PC sleeps now
		}
		if s.USBNet4 != nil && *s.USBNet4 != usbNet4(usbConf) {
			net := *s.USBNet4
			if net == "" {
				net = "default"
			}
			return d.ask(seq, "USB network?", net, "Change", func() (byte, []byte) {
				return d.change(changes)
			})
		}
		return d.change(changes)
	}
	return d.currentSettings()
}

// change makes settings changes, KEY=value lines in usbConf.
func (d *deckd) change(changes [][2]string) (byte, []byte) {
	for _, c := range changes {
		if err := setConf(usbConf, c[0], c[1]); err != nil {
			log.Printf("settings: %v", err)
			return proto.StatusFailed, []byte(err.Error())
		}
		log.Printf("settings: %s=%s", c[0], c[1])
	}
	if len(changes) > 0 {
		go d.rebind()
	}
	return d.currentSettings()
}

func (d *deckd) currentSettings() (byte, []byte) {
	on, net4 := usbDrive(usbConf), usbNet4(usbConf)
	host := d.host.get()
	s := proto.Settings{USBDrive: &on, USBNet4: &net4, HostOS: &host}
	if d.light != nil {
		if p, ok := d.light.get(); ok {
			low, sleep := brightnessMin, d.sleep.get()
			s.Brightness, s.BrightnessMin, s.SleepScreen = &p, &low, &sleep
		}
	}
	b, _ := json.Marshal(s)
	return proto.StatusOK, b
}

// usbNet is what the USB gadget setup needs, as KEY=value lines: the
// addresses from the serial at serialPath, or fallbacks without one, then
// net4 from usbConf if it's set.
func usbNet(serialPath, conf string) (string, error) {
	dev4, peer4, dev6, net6 := "172.31.254.1", "172.31.254.2", "", ""
	if b, err := os.ReadFile(serialPath); err == nil {
		id, err := netid.For(strings.ReplaceAll(string(b), "\x00", ""))
		if err != nil {
			return "", err
		}
		dev4, peer4, dev6, net6 = id.Dev4.String(), id.Peer4.String(), id.Dev6.String(), id.Net6.String()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if v := usbNet4(conf); v != "" {
		a := netip.MustParseAddr(v).As4()
		a[3] &^= 3
		a[3]++
		dev4 = netip.AddrFrom4(a).String()
		a[3]++
		peer4 = netip.AddrFrom4(a).String()
	}
	return fmt.Sprintf("DEV_IP4=%s\nPEER_IP4=%s\nDEV_IP6=%s\nNET6=%s\n", dev4, peer4, dev6, net6), nil
}

// usbIPv4 is the device's address on the USB network, now.
func usbIPv4() string {
	ifc, err := net.InterfaceByName(usbIface)
	if err != nil {
		return ""
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}

// usbIface is the USB network interface.
var usbIface = "usb0"

// rebind re-plugs the USB gadget to match the settings, after telling the
// clients. The config channel is closed after it, so it's opened again on
// the new device node; the keys' gadget is reopened on its next report.
func (d *deckd) rebind() {
	time.Sleep(rebindDelay)
	if err := d.ch.send(proto.EventSeq, proto.EventUSBRebinding, nil); err != nil {
		log.Printf("settings: %v", err)
	}
	time.Sleep(rebindDelay)
	if err := applyUSB(); err != nil {
		log.Printf("settings: applying the USB gadget: %v", err)
	}
	d.ch.set(nil)
}

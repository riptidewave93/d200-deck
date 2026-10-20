// deckhost is the PC side of the D200: a daemon that runs the button
// actions USB HID can't do (open a URL, launch a program, send an HTTP
// request), and a tray icon to control it.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/riptidewave93/d200-deck/internal/hidraw"
	"github.com/riptidewave93/d200-deck/proto"
)

// version is set at build time.
var version = "dev"

const usageText = `usage: deckhost <command>

  daemon       run the daemon (normally started by systemd)
  tray         show the tray icon

  status       show the daemon's status
  configure    open the configuration website
  update [S]   open a D200's update page (S: the end of its serial,
               needed when several are connected)
  restart      restart the daemon
  exit         stop the daemon

  hello        ask the first D200 directly who it is
  identify N   flash key N on the first D200
  host         show what the first D200 knows of its PC helper
  config get   print the first D200's config
  config put F replace it with file F
  usb-drive [on|off]
               show or set the D200's USB drive (it re-plugs to change it)
  discord-test check the Discord setup: toggles mute on and off
  version      show deckhost's version
`

func usage() {
	fmt.Fprint(os.Stderr, usageText)
	os.Exit(2)
}

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
	}
	switch cmd := os.Args[1]; cmd {
	case "daemon":
		runDaemon()
	case "tray":
		runTray()
	case "status", "configure", "update", "restart", "exit":
		if len(os.Args) > 3 || (len(os.Args) == 3 && cmd != "update") {
			usage()
		}
		reply, err := control(strings.Join(os.Args[1:], " "))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(reply)
	case "discord-test":
		if err := discordTest(); err != nil {
			log.Fatal(err)
		}
	case "version":
		fmt.Println(version)
	case "hello", "host":
		b, err := direct(map[string]byte{"hello": proto.CmdHello, "host": proto.CmdHost}[cmd], nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s\n", b)
	case "identify":
		if len(os.Args) != 3 {
			usage()
		}
		k, err := strconv.Atoi(os.Args[2])
		if err != nil {
			usage()
		}
		if _, err := direct(proto.CmdIdentify, []byte{byte(k)}); err != nil {
			log.Fatal(err)
		}
	case "config":
		if err := config(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "usb-drive":
		var payload []byte
		switch {
		case len(os.Args) == 3 && (os.Args[2] == "on" || os.Args[2] == "off"):
			payload = []byte(fmt.Sprintf(`{"usb_drive": %v}`, os.Args[2] == "on"))
		case len(os.Args) != 2:
			usage()
		}
		b, err := direct(proto.CmdSettings, payload)
		if err != nil {
			log.Fatal(err)
		}
		var s proto.Settings
		if json.Unmarshal(b, &s) != nil || s.USBDrive == nil {
			log.Fatalf("bad reply %q", b)
		}
		fmt.Println(map[bool]string{true: "on", false: "off"}[*s.USBDrive])
	default:
		usage()
	}
}

// direct sends one request to the device itself, not through the daemon.
func direct(cmd byte, payload []byte) ([]byte, error) {
	c, err := connect()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.DoWait(cmd, payload, proto.ConfirmTimeout+proto.ReplyTimeout) // the device may ask first
}

// connect opens the first D200's config channel.
func connect() (*proto.Conn, error) {
	paths := hidraw.FindAll(proto.RawDescriptors...)
	if len(paths) == 0 {
		return nil, errors.New("no D200 found")
	}
	dev, err := hidraw.Open(paths[0])
	if err != nil {
		return nil, err
	}
	return proto.NewConn(dev), nil
}

// config reads or replaces the device's config.
func config(args []string) error {
	if !(len(args) == 1 && args[0] == "get") && !(len(args) == 2 && args[0] == "put") {
		usage()
	}
	c, err := connect()
	if err != nil {
		return err
	}
	defer c.Close()
	if args[0] == "get" {
		b, _, err := c.ReadConfig()
		if err == nil {
			_, err = os.Stdout.Write(b)
		}
		return err
	}
	b, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	_, err = c.WriteConfig("", b)
	return err
}

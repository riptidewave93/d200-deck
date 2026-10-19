// deckhost is the PC side of the D200: a daemon that runs the button
// actions USB HID can't do (open a URL, launch a program, send an HTTP
// request), and a tray icon to control it.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/riptidewave93/d200-deck/internal/hidraw"
	"github.com/riptidewave93/d200-deck/proto"
)

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
	case "hello":
		b, err := direct(proto.CmdHello, nil)
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
	default:
		usage()
	}
}

// direct sends one request to the device itself, not through the daemon.
func direct(cmd byte, payload []byte) ([]byte, error) {
	paths := hidraw.FindAll(channelPrefix)
	if len(paths) == 0 {
		return nil, errors.New("no D200 found")
	}
	dev, err := hidraw.Open(paths[0])
	if err != nil {
		return nil, err
	}
	c := proto.NewConn(dev)
	defer c.Close()
	return c.Do(cmd, payload)
}

// deckhost is the PC side of the D200: it talks to the device over its config
// channel. For now it only has test commands.
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/riptidewave93/d200-deck/internal/hidraw"
	"github.com/riptidewave93/d200-deck/proto"
)

// channelPrefix starts the config channel's report descriptor (vendor usage
// page 0xffd2).
var channelPrefix = []byte{0x06, 0xd2, 0xff}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: deckhost hello | identify <key>")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	path, err := hidraw.Find(channelPrefix)
	if err != nil {
		log.Fatal(err)
	}
	dev, err := hidraw.Open(path, 2*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()
	c := proto.NewClient(dev)

	switch os.Args[1] {
	case "hello":
		b, err := c.Do(proto.CmdHello, nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %s\n", path, b)
	case "identify":
		if len(os.Args) != 3 {
			usage()
		}
		k, err := strconv.Atoi(os.Args[2])
		if err != nil {
			usage()
		}
		if _, err := c.Do(proto.CmdIdentify, []byte{byte(k)}); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
	}
}

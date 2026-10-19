// deckd is the program on the D200. Without arguments it runs its two
// parts as child processes, the keys (internal/deckd) and the update page
// (internal/deckupdate), and restarts either when it exits; the system's
// init restarts deckd itself. "deckd keys ..." and "deckd update ..." run
// one directly; "deckd usb-net", "deckd hid-descriptor" and so on print
// what they say.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/riptidewave93/d200-deck/internal/deckd"
	"github.com/riptidewave93/d200-deck/internal/deckupdate"
)

// version is set at build time.
var version = "dev"

var programs = map[string]func(args []string){
	"keys":   deckd.Main,
	"update": deckupdate.Main,
}

func main() {
	deckd.Version, deckupdate.Version = version, version
	if len(os.Args) == 1 {
		supervise([]string{"keys", "update"})
		return
	}
	run, ok := programs[os.Args[1]]
	if !ok {
		if len(os.Args) == 2 && deckd.Command(os.Args[1]) {
			return
		}
		fmt.Fprintln(os.Stderr, "usage: deckd [keys|update] [flags], or deckd icons|icons-json|fonts|default-config|hid-descriptor|raw-descriptor|usb-net")
		os.Exit(2)
	}
	log.SetPrefix(os.Args[1] + ": ") // they share deckd's log
	run(os.Args[2:])
}

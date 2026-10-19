// Package alive is how deckd shows it isn't hung: its parts beat to the
// parent over a pipe, and the parent restarts a part that stops.
package alive

import (
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Period is how often to beat.
const Period = 5 * time.Second

// Env names the fd a part's beats go to.
const Env = "DECKD_HEARTBEAT"

var (
	once sync.Once
	out  *os.File
)

// Beat says the part is alive. Call it from the loop that would hang. It does
// nothing when the part wasn't started by the parent.
func Beat() {
	once.Do(func() {
		fd, err := strconv.Atoi(os.Getenv(Env))
		if err != nil {
			return
		}
		syscall.CloseOnExec(fd) // not for the programs it runs
		out = os.NewFile(uintptr(fd), "heartbeat")
	})
	if out != nil {
		out.Write([]byte{1})
	}
}

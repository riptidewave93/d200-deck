package deckd

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/riptidewave93/d200-deck/internal/osrelease"
)

// The device has no battery-backed clock: the image starts it from
// clockFile, or its build time, and the PC sends the time (CmdTime).
var (
	clockFile = "/data/clock"     // Unix seconds, kept across reboots
	syncFile  = "/run/deckd.sync" // when the PC last sent the time, for the debug info
	clockSave = 6 * time.Hour     // how often clockFile is saved while synced: it's flash
	setClock  = func(t time.Time) error {
		tv := unix.NsecToTimeval(t.UnixNano())
		return unix.Settimeofday(&tv)
	}
)

// clock takes the time from the PC.
type clock struct {
	once sync.Once
}

// set sets the system clock to t if it's over a second off, and saves it.
func (k *clock) set(t time.Time) error {
	if b := buildTime(); t.Before(b) || t.Before(earliest) {
		return fmt.Errorf("%s is before this system was built", t.UTC().Format(time.RFC3339))
	}
	if off := time.Until(t); off > time.Second || off < -time.Second {
		if err := setClock(t); err != nil {
			return err
		}
		log.Printf("clock: set from the PC, %v off", off.Round(time.Second))
		saveClock()
	}
	os.WriteFile(syncFile, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
	k.once.Do(func() {
		saveClock()
		go func() {
			for range time.Tick(clockSave) {
				saveClock()
			}
		}()
	})
	return nil
}

// saveClock writes the time to clockFile, for the next boot.
func saveClock() {
	tmp := clockFile + ".new"
	err := os.WriteFile(tmp, fmt.Appendf(nil, "%d\n", time.Now().Unix()), 0o644)
	if err == nil {
		err = os.Rename(tmp, clockFile)
	}
	if err != nil {
		log.Printf("clock: %v", err)
	}
}

// earliest is before any build of this program.
var earliest = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// buildTime is when the running system was built, from os-release's
// BUILD_ID; zero if it's unknown.
func buildTime() time.Time {
	info, _ := osrelease.Read(osrelease.Path)
	t, _ := time.Parse("200601021504", info.Build)
	return t
}

package deckd

import (
	"os"
	"strings"
	"sync/atomic"

	"github.com/riptidewave93/d200-deck/proto"
)

// hostOS is the PC's OS as its software last said, kept in file. Keys send
// their mac_keys to a Mac.
type hostOS struct {
	file string
	v    atomic.Pointer[string]
}

func validHostOS(v string) bool { return v == "" || v == proto.HostMac || v == proto.HostOther }

func (h *hostOS) get() string {
	if p := h.v.Load(); p != nil {
		return *p
	}
	return ""
}

func (h *hostOS) mac() bool { return h.get() == proto.HostMac }

// load reads file; anything unknown is "".
func (h *hostOS) load(file string) {
	b, _ := os.ReadFile(file)
	v := strings.TrimSpace(string(b))
	if !validHostOS(v) {
		v = ""
	}
	h.file = file
	h.v.Store(&v)
}

// set takes a new value and saves it; false if v isn't one.
func (h *hostOS) set(v string) (bool, error) {
	if !validHostOS(v) {
		return false, nil
	}
	h.v.Store(&v)
	return true, saveSetting(h.file, v)
}

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"maps"

	"github.com/riptidewave93/d200-deck/proto"
)

// hostLook is how deckhost wants keys with an action state to look, such
// as the song playing (proto.HostLook).
type hostLook struct {
	rev, label string
	image      []byte
}

// setLook sets the look of keys with action state name, or clears it if
// label and image are empty, and tells the sessions if it changed.
func (d *daemon) setLook(name, label string, image []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if label == "" && len(image) == 0 {
		if _, ok := d.looks[name]; !ok {
			return
		}
		delete(d.looks, name)
	} else {
		sum := sha256.Sum256(append([]byte(label+"\x00"), image...))
		rev := hex.EncodeToString(sum[:8])
		if d.looks[name].rev == rev {
			return
		}
		d.looks[name] = hostLook{rev, label, image}
	}
	d.notifyLocked()
}

// syncLooks makes the device's looks, has (name to rev), deckhost's. It
// updates has as it goes.
func (d *daemon) syncLooks(c *proto.Conn, has map[string]string) error {
	d.mu.Lock()
	want := maps.Clone(d.looks)
	d.mu.Unlock()
	for name, l := range want {
		if has[name] != l.rev {
			if err := d.sendLook(c, name, l); err != nil {
				return err
			}
			has[name] = l.rev
		}
	}
	for name := range has {
		if _, ok := want[name]; !ok {
			if err := d.sendLook(c, name, hostLook{}); err != nil {
				return err
			}
			delete(has, name)
		}
	}
	return nil
}

func (d *daemon) sendLook(c *proto.Conn, name string, l hostLook) error {
	hl := proto.HostLook{Host: d.id, Name: name, Rev: l.rev, Label: l.label}
	if len(l.image) > 0 {
		sum := sha256.Sum256(l.image)
		hl.ImageSize, hl.ImageSHA256 = len(l.image), hex.EncodeToString(sum[:])
	}
	b, _ := json.Marshal(hl)
	if _, err := c.Do(proto.CmdHostLook, b); err != nil {
		return err
	}
	for off := 0; off < len(l.image); off += proto.MaxConfigData {
		part := binary.LittleEndian.AppendUint32(nil, uint32(off))
		part = append(part, l.image[off:min(off+proto.MaxConfigData, len(l.image))]...)
		if _, err := c.Do(proto.CmdHostLookData, part); err != nil {
			return err
		}
	}
	return nil
}

// deviceLooks reads the looks a heartbeat's reply says the device has; nil
// if it can't say (an older deckd).
func deviceLooks(reply []byte) map[string]string {
	var r proto.ActionHostReply
	if json.Unmarshal(reply, &r) != nil {
		return nil
	}
	if r.Looks == nil {
		r.Looks = map[string]string{}
	}
	return r.Looks
}

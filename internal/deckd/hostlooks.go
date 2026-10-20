package deckd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

// hostLook is how the action host wants keys with an action state to look
// (proto.HostLook).
type hostLook struct {
	rev, label string
	image      string // base64; "" for none
}

// lookUpload is a look whose image is arriving.
type lookUpload struct {
	proto.HostLook
	b []byte
}

var errBadLook = errors.New("bad host look")

// setLook takes a look from the action host: at once if it has no image,
// else once lookData has brought it all.
func (p *pcActions) setLook(l proto.HostLook) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.Host != p.host || !p.aliveLocked() {
		return nil // another host's, as with state
	}
	if l.Name == "" || len(l.Name) > 128 || len(l.Label) > proto.MaxHostLabel ||
		strings.Count(l.Label, "\n") >= deck.MaxLabelLines || l.ImageSize < 0 || l.ImageSize > proto.MaxHostImage {
		return errBadLook
	}
	if _, ok := p.looks[l.Name]; !ok && len(p.looks) >= proto.MaxHostLooks && (l.Label != "" || l.ImageSize > 0) {
		return errBadLook
	}
	p.upload = nil
	if l.ImageSize > 0 {
		p.upload = &lookUpload{HostLook: l, b: make([]byte, 0, l.ImageSize)}
		return nil
	}
	p.putLookLocked(l.Name, hostLook{rev: l.Rev, label: l.Label})
	return nil
}

// lookData takes part of the image of the look being set.
func (p *pcActions) lookData(off int, b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	u := p.upload
	if u == nil || off != len(u.b) || len(u.b)+len(b) > u.ImageSize {
		return errBadLook
	}
	u.b = append(u.b, b...)
	if len(u.b) < u.ImageSize {
		return nil
	}
	p.upload = nil
	if sum := sha256.Sum256(u.b); hex.EncodeToString(sum[:]) != u.ImageSHA256 {
		return errBadLook
	}
	p.putLookLocked(u.Name, hostLook{rev: u.Rev, label: u.Label, image: base64.StdEncoding.EncodeToString(u.b)})
	return nil
}

// putLookLocked sets look name, or clears it if l is empty.
func (p *pcActions) putLookLocked(name string, l hostLook) {
	if p.looks == nil {
		p.looks = map[string]hostLook{}
	}
	if l.label == "" && l.image == "" {
		delete(p.looks, name)
	} else {
		p.looks[name] = l
	}
	p.notify()
}

// look is the action host's look for keys with action state name.
func (p *pcActions) look(name string) (hostLook, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if name == "" || !p.aliveLocked() {
		return hostLook{}, false
	}
	l, ok := p.looks[name]
	return l, ok
}

// lookRevs are the looks shown, for the reply to the host's heartbeat.
func (p *pcActions) lookRevs() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	revs := map[string]string{}
	for name, l := range p.looks {
		revs[name] = l.rev
	}
	return revs
}

// withLook is b as the host's look l (for action state name) shows it: its
// image filling the key, or its label.
func (d *deckd) withLook(b deck.Button, name string, l hostLook) deck.Button {
	b.Icon, b.Image, b.Label = "", "", l.label
	if l.image != "" {
		id := "\x00host:" + name + ":" + l.rev // new with each version: no stale cache
		if d.renderer.ExtraImages == nil || len(d.renderer.ExtraImages) > 2*proto.MaxHostLooks {
			d.renderer.ExtraImages = map[string]string{}
		}
		d.renderer.ExtraImages[id] = l.image
		b.Image, b.Label = id, ""
	}
	return b
}

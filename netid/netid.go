// Package netid derives a D200's USB network addresses and mDNS name from
// its serial, so every deck on a PC gets its own link without any lookup.
// The website ports this to JavaScript; testdata/vectors.json keeps both in
// step.
package netid

import (
	"crypto/sha256"
	"errors"
	"net/netip"
	"strings"
)

// ID is a deck's addresses on its USB Ethernet link.
type ID struct {
	Dev4   netip.Addr   // the deck
	Peer4  netip.Addr   // the PC, handed out by DHCP
	Net4   netip.Prefix // a /30 in 198.18.0.0/15
	Dev6   netip.Addr   // the deck; the PC takes an address by SLAAC
	Net6   netip.Prefix // a ULA /64
	MDNS   string       // d200-<serial>.local
	Serial string       // lower case
}

// For derives the ID of the deck with this serial (hex, any case).
func For(serial string) (ID, error) {
	serial = strings.ToLower(strings.TrimSpace(serial))
	if serial == "" || len(serial) > 64 || strings.Trim(serial, "0123456789abcdef") != "" {
		return ID{}, errors.New("serial must be 1-64 hex digits")
	}
	h := sha256.Sum256([]byte("d200-net\x00" + serial))

	// IPv4: the top 15 bits pick one of 32768 /30s in 198.18.0.0/15
	idx := uint32(h[0])<<7 | uint32(h[1])>>1
	base := uint32(198)<<24 | uint32(18)<<16 | idx<<2
	v4 := func(n uint32) netip.Addr {
		return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
	}

	// IPv6: fd, a 40-bit global ID from bytes 2-6, subnet 0
	var p [16]byte
	p[0] = 0xfd
	copy(p[1:6], h[2:7])
	dev6 := p
	dev6[15] = 1

	return ID{
		Dev4:   v4(base + 1),
		Peer4:  v4(base + 2),
		Net4:   netip.PrefixFrom(v4(base), 30),
		Dev6:   netip.AddrFrom16(dev6),
		Net6:   netip.PrefixFrom(netip.AddrFrom16(p), 64),
		MDNS:   "d200-" + serial + ".local",
		Serial: serial,
	}, nil
}

// UpdateURL is the deck's update page (served by deck-update). It uses the
// IPv4 address: browsers in a sandbox (e.g. Flatpak) often can't resolve
// .local names.
func (id ID) UpdateURL() string { return "http://" + id.Dev4.String() + "/update" }

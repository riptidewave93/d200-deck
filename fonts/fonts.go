// Package fonts makes fonts small enough to carry in a deck config.
package fonts

import (
	"cmp"
	"encoding/binary"
	"errors"
	"slices"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var (
	// ErrNotTrueType is a font that isn't TrueType: OpenType with CFF
	// outlines, or a collection.
	ErrNotTrueType = errors.New("not a TrueType font (CFF outlines and collections aren't supported)")
	errBad         = errors.New("bad TrueType font")
)

// Tables copied as they are; the rest are rebuilt or dropped.
var copied = []string{"OS/2", "cvt ", "fpgm", "gasp", "name", "prep"}

// Subset is TrueType font ttf with only the glyphs runes need, plus the
// missing-glyph box and the parts of composite glyphs. Runes the font
// doesn't have are left out. It keeps the kerning between them, and drops
// the other layout tables (ligatures, GSUB and GPOS).
func Subset(ttf []byte, runes []rune) ([]byte, error) {
	t, err := readTables(ttf)
	if err != nil {
		return nil, err
	}
	f, err := sfnt.Parse(ttf)
	if err != nil {
		return nil, err
	}
	head, hhea, hmtx, maxp := t["head"], t["hhea"], t["hmtx"], t["maxp"]
	if len(head) < 54 || len(hhea) < 36 || len(maxp) < 6 {
		return nil, errBad
	}
	numGlyphs := int(u16(maxp[4:]))
	loca, err := locations(t["loca"], u16(head[50:]) != 0, numGlyphs, len(t["glyf"]))
	if err != nil {
		return nil, err
	}
	glyph := func(g uint16) []byte { return t["glyf"][loca[g]:loca[g+1]] }

	// the glyphs kept, by new ID; the characters, by old glyph ID
	keep := []uint16{0}
	newID := map[uint16]uint16{0: 0}
	add := func(g uint16) {
		if _, ok := newID[g]; !ok {
			newID[g] = uint16(len(keep))
			keep = append(keep, g)
		}
	}
	runes = slices.Clone(runes)
	slices.Sort(runes)
	runes = slices.Compact(runes)
	var buf sfnt.Buffer
	chars := map[rune]uint16{}
	for _, r := range runes {
		if g, err := f.GlyphIndex(&buf, r); err == nil && g != 0 && int(g) < numGlyphs {
			add(uint16(g))
			chars[r] = uint16(g)
		}
	}
	shown := slices.Clone(keep[1:]) // kerned: the glyphs characters map to
	for i := 0; i < len(keep); i++ {
		parts, err := components(glyph(keep[i]))
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			if int(p.gid) >= numGlyphs {
				return nil, errBad
			}
			add(p.gid)
		}
	}
	if len(keep) > 0xffff {
		return nil, errBad
	}

	// glyf, loca and hmtx for the kept glyphs, in their new order
	numH := int(u16(hhea[34:]))
	if numH == 0 || len(hmtx) < 4*numH+2*(numGlyphs-numH) {
		return nil, errBad
	}
	var glyf, newLoca, newHmtx []byte
	for _, g := range keep {
		newLoca = binary.BigEndian.AppendUint32(newLoca, uint32(len(glyf)))
		data := slices.Clone(glyph(g))
		parts, _ := components(data)
		for _, p := range parts {
			binary.BigEndian.PutUint16(data[p.at:], newID[p.gid])
		}
		glyf = append(glyf, data...)
		for len(glyf)%4 != 0 {
			glyf = append(glyf, 0)
		}
		adv, lsb := hmtx[4*(numH-1):], hmtx[4*numH+2*(int(g)-numH):]
		if int(g) < numH {
			adv, lsb = hmtx[4*int(g):], hmtx[4*int(g)+2:]
		}
		newHmtx = append(newHmtx, adv[0], adv[1], lsb[0], lsb[1])
	}
	newLoca = binary.BigEndian.AppendUint32(newLoca, uint32(len(glyf)))

	out := map[string][]byte{
		"cmap": cmap(runes, chars, newID),
		"glyf": glyf,
		"loca": newLoca,
		"hmtx": newHmtx,
		"head": slices.Clone(head[:54]),
		"hhea": slices.Clone(hhea[:36]),
		"maxp": slices.Clone(maxp),
		"post": post(t["post"]),
	}
	binary.BigEndian.PutUint16(out["head"][50:], 1) // long loca offsets
	binary.BigEndian.PutUint16(out["hhea"][34:], uint16(len(keep)))
	binary.BigEndian.PutUint16(out["maxp"][4:], uint16(len(keep)))
	if k := kern(f, shown, newID); k != nil {
		out["kern"] = k
	}
	for _, tag := range copied {
		if b, ok := t[tag]; ok {
			out[tag] = b
		}
	}
	return write(out), nil
}

// readTables reads ttf's table directory.
func readTables(ttf []byte) (map[string][]byte, error) {
	if len(ttf) < 12 {
		return nil, errBad
	}
	switch string(ttf[:4]) {
	case "\x00\x01\x00\x00", "true":
	case "OTTO", "ttcf":
		return nil, ErrNotTrueType
	default:
		return nil, errBad
	}
	n := int(u16(ttf[4:]))
	if len(ttf) < 12+16*n {
		return nil, errBad
	}
	t := map[string][]byte{}
	for i := range n {
		rec := ttf[12+16*i:]
		off, size := int64(u32(rec[8:])), int64(u32(rec[12:]))
		if off+size > int64(len(ttf)) {
			return nil, errBad
		}
		t[string(rec[:4])] = ttf[off : off+size]
	}
	for _, tag := range []string{"cmap", "glyf", "head", "hhea", "hmtx", "loca", "maxp"} {
		if _, ok := t[tag]; !ok {
			if tag == "glyf" {
				return nil, ErrNotTrueType
			}
			return nil, errBad
		}
	}
	return t, nil
}

// locations are the numGlyphs+1 offsets of the glyphs in glyf.
func locations(loca []byte, long bool, numGlyphs, glyfLen int) ([]int, error) {
	size := 2
	if long {
		size = 4
	}
	if len(loca) < size*(numGlyphs+1) {
		return nil, errBad
	}
	offs := make([]int, numGlyphs+1)
	for i := range offs {
		if long {
			o := u32(loca[4*i:])
			if o > uint32(glyfLen) { // before int: it may be 32 bits
				return nil, errBad
			}
			offs[i] = int(o)
		} else {
			offs[i] = 2 * int(u16(loca[2*i:]))
		}
		if offs[i] > glyfLen || i > 0 && offs[i] < offs[i-1] {
			return nil, errBad
		}
	}
	return offs, nil
}

// component is a glyph that composite glyph uses, and where its ID is.
type component struct {
	gid uint16
	at  int
}

// components lists composite glyph data's parts; none for a simple glyph.
func components(data []byte) ([]component, error) {
	if len(data) < 10 || int16(u16(data)) >= 0 {
		return nil, nil
	}
	const (
		argWords = 0x0001
		scale    = 0x0008
		more     = 0x0020
		xyScale  = 0x0040
		twoByTwo = 0x0080
	)
	var parts []component
	for i := 10; ; {
		if i+4 > len(data) {
			return nil, errBad
		}
		flags := u16(data[i:])
		parts = append(parts, component{u16(data[i+2:]), i + 2})
		i += 4 + 2
		if flags&argWords != 0 {
			i += 2
		}
		switch {
		case flags&scale != 0:
			i += 2
		case flags&xyScale != 0:
			i += 4
		case flags&twoByTwo != 0:
			i += 8
		}
		if flags&more == 0 {
			return parts, nil
		}
	}
}

// cmap maps runes to the new glyph IDs: one format 12 subtable, for
// Unicode, in a Windows encoding record.
func cmap(runes []rune, chars map[rune]uint16, newID map[uint16]uint16) []byte {
	type group struct{ start, end, gid uint32 }
	var groups []group
	for _, r := range runes {
		g, ok := chars[r]
		if !ok {
			continue
		}
		id := uint32(newID[g])
		if n := len(groups); n > 0 && groups[n-1].end+1 == uint32(r) && groups[n-1].gid+uint32(r)-groups[n-1].start == id {
			groups[n-1].end++
			continue
		}
		groups = append(groups, group{uint32(r), uint32(r), id})
	}
	b := []byte{0, 0, 0, 1, 0, 3, 0, 10, 0, 0, 0, 12} // version 0, 1 table: (3, 10) at 12
	b = binary.BigEndian.AppendUint16(b, 12)
	b = binary.BigEndian.AppendUint16(b, 0)
	b = binary.BigEndian.AppendUint32(b, uint32(16+12*len(groups)))
	b = binary.BigEndian.AppendUint32(b, 0)
	b = binary.BigEndian.AppendUint32(b, uint32(len(groups)))
	for _, g := range groups {
		b = binary.BigEndian.AppendUint32(b, g.start)
		b = binary.BigEndian.AppendUint32(b, g.end)
		b = binary.BigEndian.AppendUint32(b, g.gid)
	}
	return b
}

// post is post table p as version 3: no glyph names.
func post(p []byte) []byte {
	b := make([]byte, 32)
	copy(b, p)
	binary.BigEndian.PutUint32(b, 0x00030000)
	return b
}

// maxKerned bounds the glyphs kerning is kept for: each pair is looked up.
const maxKerned = 1024

// kern is a kern table with the kerning f has between glyphs (from its kern
// or GPOS table), or nil if there's none.
func kern(f *sfnt.Font, glyphs []uint16, newID map[uint16]uint16) []byte {
	if len(glyphs) > maxKerned {
		return nil
	}
	ppem := fixed.Int26_6(f.UnitsPerEm()) << 6 // so it's in font units
	var buf sfnt.Buffer
	type pair struct {
		l, r uint16
		v    int16
	}
	var pairs []pair
	for _, l := range glyphs {
		for _, r := range glyphs {
			k, err := f.Kern(&buf, sfnt.GlyphIndex(l), sfnt.GlyphIndex(r), ppem, font.HintingNone)
			if err == nil && k != 0 {
				pairs = append(pairs, pair{newID[l], newID[r], int16(k.Round())})
			}
		}
	}
	if len(pairs) == 0 || 14+6*len(pairs) > 0xffff {
		return nil
	}
	slices.SortFunc(pairs, func(a, b pair) int { return cmp.Or(cmp.Compare(a.l, b.l), cmp.Compare(a.r, b.r)) })
	search, sel := 1, 0
	for search*2 <= len(pairs) {
		search, sel = search*2, sel+1
	}
	b := []byte{0, 0, 0, 1, 0, 0} // version 0, 1 table; its version 0
	for _, v := range []int{14 + 6*len(pairs), 1, len(pairs), 6 * search, sel, 6 * (len(pairs) - search)} {
		b = binary.BigEndian.AppendUint16(b, uint16(v))
	}
	for _, p := range pairs {
		b = binary.BigEndian.AppendUint16(b, p.l)
		b = binary.BigEndian.AppendUint16(b, p.r)
		b = binary.BigEndian.AppendUint16(b, uint16(p.v))
	}
	return b
}

// write lays tables out as a TrueType font.
func write(tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	n := len(tags)
	search, sel := 1, 0
	for search*2 <= n {
		search, sel = search*2, sel+1
	}
	b := []byte{0, 1, 0, 0}
	for _, v := range []int{n, 16 * search, sel, 16 * (n - search)} {
		b = binary.BigEndian.AppendUint16(b, uint16(v))
	}
	off := 12 + 16*n
	var body []byte
	headAt := 0
	for _, tag := range tags {
		t := tables[tag]
		if tag == "head" {
			headAt = off + len(body)
			binary.BigEndian.PutUint32(t[8:], 0)
		}
		b = append(b, tag...)
		b = binary.BigEndian.AppendUint32(b, checksum(t))
		b = binary.BigEndian.AppendUint32(b, uint32(off+len(body)))
		b = binary.BigEndian.AppendUint32(b, uint32(len(t)))
		body = append(body, t...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	b = append(b, body...)
	binary.BigEndian.PutUint32(b[headAt+8:], 0xb1b0afba-checksum(b))
	return b
}

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		sum += binary.BigEndian.Uint32(w[:])
	}
	return sum
}

func u16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }
func u32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

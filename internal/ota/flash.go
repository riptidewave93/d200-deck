package ota

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// NAND is an MTD partition.
type NAND interface {
	Size() int64
	EraseSize() int64
	PageSize() int // data bytes per page
	OOBSize() int
	Erase(off int64) error // one block
	IsBad(off int64) (bool, error)
	MarkBad(off int64) error
	ReadAt(b []byte, off int64) (int, error)  // with ECC
	WriteAt(b []byte, off int64) (int, error) // with ECC; whole pages
	ReadRaw(b []byte, off int64) error        // without ECC, data only
	WritePageRaw(off int64, data, oob []byte) error
}

// Result is what happened to one target.
type Result struct {
	Target     string `json:"target"`
	Name       string `json:"name,omitempty"` // as people see it, e.g. System
	Written    bool   `json:"written"`
	Skipped    bool   `json:"skipped,omitempty"` // it already matched
	Retries    int    `json:"retries,omitempty"`
	MarkedBad  int    `json:"marked_bad,omitempty"`  // blocks marked bad
	IDBInvalid int    `json:"idb_invalid,omitempty"` // IDB copies left invalid
	Error      string `json:"error,omitempty"`
}

func (r *Result) fail(err error) error {
	r.Error = err.Error()
	return err
}

// IDB layout: 4 copies, each a header block then a code block.
const (
	idbCopies = 4
	idbStride = 0x40000
	idbCode   = 0x20000
	idbMagic  = 4 // bytes
)

// checkIDB refuses an IDB image with data outside its header page and code
// block, which the copies don't hold.
func checkIDB(img []byte, page int) error {
	if len(img) != idbSize {
		return errors.New("idbloader: wrong size")
	}
	for _, b := range img[page:idbCode] {
		if b != 0xff {
			return errors.New("idbloader: data between the header page and the code block")
		}
	}
	return nil
}

// WriteIDB writes the 4 IDB copies, last first, so that a power cut leaves
// every copy either invalid (no magic) or complete.
func WriteIDB(dev NAND, img []byte) (res Result) {
	res.Target = "idbloader"
	page := dev.PageSize()
	if err := checkIDB(img, page); err != nil {
		res.fail(err)
		return
	}
	if idbMatches(dev, img) {
		res.Skipped = true
		return
	}
	for k := idbCopies - 1; k >= 0; k-- {
		base := int64(k * idbStride)
		err := writeIDBCopy(dev, base, img)
		if err != nil {
			res.Retries++
			err = writeIDBCopy(dev, base, img)
		}
		if err != nil {
			dev.Erase(base) // leave it invalid
			res.IDBInvalid++
			if idbCopies-res.IDBInvalid < 2 {
				res.fail(fmt.Errorf("idbloader: copy %d: %w; fewer than 2 good copies", k, err))
				return
			}
		}
	}
	res.Written = true
	return
}

func idbMatches(dev NAND, img []byte) bool {
	page := dev.PageSize()
	for k := range idbCopies {
		base := int64(k * idbStride)
		if !rawMatches(dev, base, img[:page]) || !matches(dev, base+idbCode, img[idbCode:]) {
			return false
		}
	}
	return true
}

func writeIDBCopy(dev NAND, base int64, img []byte) error {
	page := dev.PageSize()
	if err := dev.Erase(base); err != nil {
		return err
	}
	if err := dev.Erase(base + idbCode); err != nil {
		return err
	}
	if _, err := dev.WriteAt(img[idbCode:], base+idbCode); err != nil {
		return err
	}
	if !matches(dev, base+idbCode, img[idbCode:]) {
		return errors.New("code block doesn't read back")
	}
	// the header page in two raw passes: everything but the magic, then the
	// magic, so the BootROM never sees a magic before the rest is there
	header := img[:page]
	oob := bytes.Repeat([]byte{0xff}, dev.OOBSize())
	first := bytes.Clone(header)
	copy(first, []byte{0xff, 0xff, 0xff, 0xff})
	if err := dev.WritePageRaw(base, first, oob); err != nil {
		return err
	}
	magic := bytes.Repeat([]byte{0xff}, page)
	copy(magic, header[:idbMagic])
	if err := dev.WritePageRaw(base, magic, oob); err != nil {
		return err
	}
	if !rawMatches(dev, base, header) {
		return errors.New("header page doesn't read back")
	}
	return nil
}

func matches(dev NAND, off int64, want []byte) bool {
	got := make([]byte, len(want))
	n, err := dev.ReadAt(got, off)
	return err == nil && n == len(want) && bytes.Equal(got, want)
}

func rawMatches(dev NAND, off int64, want []byte) bool {
	got := make([]byte, len(want))
	return dev.ReadRaw(got, off) == nil && bytes.Equal(got, want)
}

// WriteUBoot writes img to u-boot-backup, then u-boot.
func WriteUBoot(backup, main NAND, img []byte) (res Result) {
	res.Target = "u-boot"
	if partMatches(backup, img) && partMatches(main, img) {
		res.Skipped = true
		return
	}
	for _, p := range []struct {
		name string
		dev  NAND
	}{{"u-boot-backup", backup}, {"u-boot", main}} {
		err := writePart(p.dev, img, &res)
		if err != nil {
			res.Retries++
			err = writePart(p.dev, img, &res)
		}
		if err != nil {
			if p.dev == main {
				eraseAll(main) // SPL falls back to the backup, just written
			}
			res.fail(fmt.Errorf("%s: %w", p.name, err))
			return
		}
	}
	res.Written = true
	return
}

// goodBlocks are the offsets of the partition's good blocks.
func goodBlocks(dev NAND) ([]int64, error) {
	var good []int64
	for off := int64(0); off < dev.Size(); off += dev.EraseSize() {
		bad, err := dev.IsBad(off)
		if err != nil {
			return nil, err
		}
		if !bad {
			good = append(good, off)
		}
	}
	return good, nil
}

// partMatches reports whether img is in the partition's good blocks.
func partMatches(dev NAND, img []byte) bool {
	good, err := goodBlocks(dev)
	if err != nil {
		return false
	}
	bs := dev.EraseSize()
	for i, rest := 0, img; len(rest) > 0; i++ {
		if i == len(good) {
			return false
		}
		n := min(int64(len(rest)), bs)
		if !matches(dev, good[i], rest[:n]) {
			return false
		}
		rest = rest[n:]
	}
	return true
}

// writePart erases the partition's good blocks and writes img into them,
// marking a block that fails to erase or write bad.
func writePart(dev NAND, img []byte, res *Result) error {
	good, err := goodBlocks(dev)
	if err != nil {
		return err
	}
	bs := dev.EraseSize()
	if int64(len(good))*bs < int64(len(img)) {
		return errors.New("not enough good blocks")
	}
	markBad := func(off int64, err error) error {
		if dev.MarkBad(off) == nil {
			res.MarkedBad++
		}
		return err
	}
	for _, off := range good {
		if err := dev.Erase(off); err != nil {
			return markBad(off, err)
		}
	}
	for i, rest := 0, img; len(rest) > 0; i++ {
		n := min(int64(len(rest)), bs)
		if _, err := dev.WriteAt(padPage(rest[:n], dev.PageSize()), good[i]); err != nil {
			return markBad(good[i], err)
		}
		rest = rest[n:]
	}
	if !partMatches(dev, img) {
		return errors.New("doesn't read back")
	}
	return nil
}

// padPage pads b with 0xff to whole pages.
func padPage(b []byte, page int) []byte {
	if r := len(b) % page; r != 0 {
		b = append(bytes.Clone(b), bytes.Repeat([]byte{0xff}, page-r)...)
	}
	return b
}

func eraseAll(dev NAND) {
	good, _ := goodBlocks(dev)
	for _, off := range good {
		dev.Erase(off)
	}
}

// UBI is the UBI device holding the slots.
type UBI interface {
	// Volume returns a volume's reserved size, and whether it exists.
	Volume(id int) (bytes int64, ok bool, err error)
	Create(v Volume) error
	Resize(id int, bytes int64) error
	// Update replaces a volume's contents with size bytes from r (size 0
	// empties it).
	Update(id int, r io.Reader, size int64) error
	Open(id int) (io.ReadCloser, error)
}

// Volume is a slot's UBI volume, as laid out on the flash.
type Volume struct {
	ID     int
	Name   string
	Static bool
	Bytes  int64
}

// SlotVolumes are the kernel and rootfs volumes of slot "a" or "b".
func SlotVolumes(slot string) (kernel, rootfs Volume) {
	b := 0
	if slot == "b" {
		b = 1
	}
	return Volume{2 + b, "kernel_" + slot, true, 8 << 20}, Volume{4 + b, "rootfs_" + slot, false, 32 << 20}
}

// WriteVolume writes image file path into volume v, creating or growing it
// first. A static volume (a kernel) that already holds the image is left as
// it is: the kernel rarely changes between updates.
func WriteVolume(u UBI, v Volume, target, path string, im Image) (res Result) {
	res.Target = target
	size, ok, err := u.Volume(v.ID)
	if err == nil && ok && v.Static && size >= im.Size && volumeHolds(u, v.ID, im) == nil {
		res.Skipped = true
		return
	}
	if err == nil && !ok {
		err = u.Create(v)
		size = v.Bytes
	}
	if err == nil && size < im.Size {
		err = u.Resize(v.ID, im.Size)
	}
	if err != nil {
		res.fail(fmt.Errorf("%s: %w", v.Name, err))
		return
	}
	err = writeVolume(u, v.ID, path, im)
	if err != nil {
		res.Retries++
		err = writeVolume(u, v.ID, path, im)
	}
	if err != nil {
		u.Update(v.ID, nil, 0) // leave it empty
		res.fail(fmt.Errorf("%s: %w", v.Name, err))
		return
	}
	res.Written = true
	return
}

func writeVolume(u UBI, id int, path string, im Image) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := u.Update(id, f, im.Size); err != nil {
		return err
	}
	return volumeHolds(u, id, im)
}

// volumeHolds checks volume id starts with image im.
func volumeHolds(u UBI, id int, im Image) error {
	r, err := u.Open(id)
	if err != nil {
		return err
	}
	defer r.Close()
	h := sha256.New()
	if n, err := io.Copy(h, io.LimitReader(r, im.Size)); err != nil || n != im.Size {
		return fmt.Errorf("reading back: %d bytes, %v", n, err)
	}
	if hex.EncodeToString(h.Sum(nil)) != im.SHA256 {
		return errors.New("doesn't read back")
	}
	return nil
}

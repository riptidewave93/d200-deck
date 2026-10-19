package ota

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// fakeNAND is an MTD partition in memory. Like NAND, a write can only clear
// bits; ECC reads of a page written raw fail.
type fakeNAND struct {
	page, oob int
	block     int64
	data      []byte
	raw       map[int64]bool // pages written raw
	bad       map[int64]bool
	failErase map[int64]int // block: failures left
	failWrite map[int64]int
	erases    int
}

func newNAND(size int64) *fakeNAND {
	return &fakeNAND{page: 2048, oob: 128, block: 128 << 10, data: bytes.Repeat([]byte{0xff}, int(size)),
		raw: map[int64]bool{}, bad: map[int64]bool{}, failErase: map[int64]int{}, failWrite: map[int64]int{}}
}

var errIO = errors.New("I/O error")

func (n *fakeNAND) Size() int64      { return int64(len(n.data)) }
func (n *fakeNAND) EraseSize() int64 { return n.block }
func (n *fakeNAND) PageSize() int    { return n.page }
func (n *fakeNAND) OOBSize() int     { return n.oob }

func (n *fakeNAND) fails(m map[int64]int, off int64) bool {
	b := off / n.block * n.block
	if m[b] > 0 {
		m[b]--
		return true
	}
	return false
}

func (n *fakeNAND) Erase(off int64) error {
	if off%n.block != 0 {
		return syscall.EINVAL
	}
	n.erases++
	if n.fails(n.failErase, off) {
		return errIO
	}
	copy(n.data[off:off+n.block], bytes.Repeat([]byte{0xff}, int(n.block)))
	for p := off; p < off+n.block; p += int64(n.page) {
		delete(n.raw, p)
	}
	return nil
}

func (n *fakeNAND) IsBad(off int64) (bool, error) { return n.bad[off/n.block*n.block], nil }
func (n *fakeNAND) MarkBad(off int64) error       { n.bad[off/n.block*n.block] = true; return nil }

func (n *fakeNAND) program(b []byte, off int64) error {
	if off%int64(n.page) != 0 || len(b)%n.page != 0 || off+int64(len(b)) > n.Size() {
		return syscall.EINVAL
	}
	if n.fails(n.failWrite, off) {
		return errIO
	}
	for i, v := range b {
		n.data[off+int64(i)] &= v
	}
	return nil
}

func (n *fakeNAND) WriteAt(b []byte, off int64) (int, error) {
	if err := n.program(b, off); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (n *fakeNAND) WritePageRaw(off int64, data, oob []byte) error {
	if len(data) != n.page || len(oob) != n.oob {
		return syscall.EINVAL
	}
	if err := n.program(data, off); err != nil {
		return err
	}
	n.raw[off] = true
	return nil
}

func (n *fakeNAND) ReadAt(b []byte, off int64) (int, error) {
	for p := off / int64(n.page) * int64(n.page); p < off+int64(len(b)); p += int64(n.page) {
		if n.raw[p] {
			return 0, syscall.EBADMSG
		}
	}
	return copy(b, n.data[off:]), nil
}

func (n *fakeNAND) ReadRaw(b []byte, off int64) error {
	copy(b, n.data[off:])
	return nil
}

func idbImage() []byte {
	img := bytes.Repeat([]byte{0xff}, idbSize)
	copy(img, "RKNS")
	for i := idbMagic; i < 2048; i++ {
		img[i] = byte(i)
	}
	for i := idbCode; i < idbSize; i++ {
		img[i] = byte(i * 7)
	}
	return img
}

func TestWriteIDB(t *testing.T) {
	dev, img := newNAND(1<<20), idbImage()
	res := WriteIDB(dev, img)
	if !res.Written || res.Error != "" || res.Retries != 0 {
		t.Fatalf("%+v", res)
	}
	for k := range idbCopies {
		base := k * idbStride
		if !bytes.Equal(dev.data[base:base+idbStride], img) {
			t.Errorf("copy %d differs", k)
		}
	}
	if _, err := dev.ReadAt(make([]byte, 16), 0); !errors.Is(err, syscall.EBADMSG) {
		t.Error("the fake should fail ECC reads of header pages")
	}
	before := dev.erases
	if res := WriteIDB(dev, img); !res.Skipped || dev.erases != before {
		t.Errorf("rewrote matching copies: %+v", res)
	}
}

func TestWriteIDBFailures(t *testing.T) {
	// copy 2's header block fails to erase twice: it's left invalid
	dev, img := newNAND(1<<20), idbImage()
	dev.failErase[2*idbStride] = 2
	res := WriteIDB(dev, img)
	if !res.Written || res.IDBInvalid != 1 || res.Retries != 1 {
		t.Fatalf("%+v", res)
	}
	if !bytes.Equal(dev.data[2*idbStride:2*idbStride+2048], bytes.Repeat([]byte{0xff}, 2048)) {
		t.Error("failed copy's header not erased")
	}

	// three copies fail: stop, leaving copy 0 as it was
	dev = newNAND(1 << 20)
	for k := 1; k < 4; k++ {
		dev.failWrite[int64(k*idbStride+idbCode)] = 2
	}
	if res := WriteIDB(dev, img); res.Written || res.Error == "" || res.IDBInvalid != 3 || !bytes.Equal(dev.data[:idbStride], bytes.Repeat([]byte{0xff}, idbStride)) {
		t.Errorf("%+v", res)
	}

	// a write that fails once is retried
	dev = newNAND(1 << 20)
	dev.failWrite[0] = 1
	if res := WriteIDB(dev, img); !res.Written || res.Retries != 1 || res.IDBInvalid != 0 {
		t.Errorf("%+v", res)
	}

	bad := idbImage()
	bad[0x10000] = 0
	if res := WriteIDB(newNAND(1<<20), bad); res.Error == "" {
		t.Error("image with data in the gap accepted")
	}
}

func ubootImage() []byte {
	img := make([]byte, 1217536+100) // not a whole page
	for i := range img {
		img[i] = byte(i * 13)
	}
	return img
}

func TestWriteUBoot(t *testing.T) {
	backup, main, img := newNAND(4<<20), newNAND(4<<20), ubootImage()
	backup.bad[3*backup.block] = true
	res := WriteUBoot(backup, main, img)
	if !res.Written || res.Error != "" {
		t.Fatalf("%+v", res)
	}
	if !partMatches(backup, img) || !partMatches(main, img) {
		t.Error("partitions don't hold the image")
	}
	if backup.data[3*backup.block] != 0xff {
		t.Error("wrote into a bad block")
	}
	if res := WriteUBoot(backup, main, img); !res.Skipped {
		t.Errorf("%+v", res)
	}

	// a block that fails to write is marked bad and the partition redone
	backup, main = newNAND(4<<20), newNAND(4<<20)
	main.failWrite[main.block] = 1
	res = WriteUBoot(backup, main, img)
	if !res.Written || res.MarkedBad != 1 || res.Retries != 1 || !main.bad[main.block] || !partMatches(main, img) {
		t.Errorf("%+v", res)
	}

	// u-boot fails twice: it's erased so SPL uses the backup
	backup, main = newNAND(4<<20), newNAND(4<<20)
	main.failWrite[0], main.failWrite[main.block] = 1, 1
	res = WriteUBoot(backup, main, img)
	if res.Written || res.Error == "" || !partMatches(backup, img) {
		t.Errorf("%+v", res)
	}
	for off := range main.Size() / main.block {
		if !main.bad[off*main.block] && main.data[off*main.block] != 0xff {
			t.Fatal("failed u-boot not erased")
		}
	}

	// the backup fails twice: stop before touching u-boot
	backup, main = newNAND(4<<20), newNAND(4<<20)
	backup.failErase[0], backup.failErase[backup.block] = 1, 1
	if res := WriteUBoot(backup, main, img); res.Error == "" || main.erases != 0 {
		t.Errorf("%+v, %d erases of u-boot", res, main.erases)
	}

	// too many bad blocks
	backup = newNAND(1 << 20)
	if res := WriteUBoot(backup, newNAND(4<<20), img); res.Error == "" {
		t.Errorf("%+v", res)
	}
}

// fakeUBI keeps volumes in memory.
type fakeUBI struct {
	vols       map[int]*fakeVol
	failUpdate int // updates left to fail
	corrupt    int // read backs left to corrupt
}

type fakeVol struct {
	v    Volume
	data []byte
}

func (u *fakeUBI) Volume(id int) (int64, bool, error) {
	v, ok := u.vols[id]
	if !ok {
		return 0, false, nil
	}
	return v.v.Bytes, true, nil
}

func (u *fakeUBI) Create(v Volume) error {
	if _, ok := u.vols[v.ID]; ok {
		return syscall.EEXIST
	}
	u.vols[v.ID] = &fakeVol{v: v}
	return nil
}

func (u *fakeUBI) Resize(id int, bytes int64) error {
	u.vols[id].v.Bytes = bytes
	return nil
}

func (u *fakeUBI) Update(id int, r io.Reader, size int64) error {
	v := u.vols[id]
	if size > v.v.Bytes {
		return syscall.ENOSPC
	}
	if u.failUpdate > 0 {
		u.failUpdate--
		return errIO
	}
	v.data = make([]byte, size)
	if size > 0 {
		_, err := io.ReadFull(r, v.data)
		return err
	}
	return nil
}

func (u *fakeUBI) Open(id int) (io.ReadCloser, error) {
	d := bytes.Clone(u.vols[id].data)
	if u.corrupt > 0 && len(d) > 0 {
		u.corrupt--
		d[0]++
	}
	if !u.vols[id].v.Static { // a dynamic volume reads back whole
		d = append(d, make([]byte, 64)...)
	}
	return io.NopCloser(bytes.NewReader(d)), nil
}

func imageFile(t *testing.T, data []byte) (string, Image) {
	path := filepath.Join(t.TempDir(), "img")
	os.WriteFile(path, data, 0o644)
	_, im := imageFileData(data)
	return path, im
}

func imageFileData(data []byte) (string, Image) {
	sum := sha256.Sum256(data)
	return "", Image{File: "img", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func TestWriteVolume(t *testing.T) {
	kernel, rootfs := SlotVolumes("b")
	if kernel.ID != 3 || rootfs.ID != 5 || !kernel.Static || rootfs.Static || rootfs.Name != "rootfs_b" {
		t.Fatalf("slot b: %+v %+v", kernel, rootfs)
	}
	if k, r := SlotVolumes("a"); k.ID != 2 || r.ID != 4 {
		t.Fatal("slot a")
	}

	u := &fakeUBI{vols: map[int]*fakeVol{}}
	path, im := imageFile(t, []byte("a rootfs image"))
	if res := WriteVolume(u, rootfs, "rootfs", path, im); !res.Written || res.Error != "" {
		t.Fatalf("%+v", res)
	}
	if string(u.vols[5].data) != "a rootfs image" || u.vols[5].v.Bytes != 32<<20 {
		t.Errorf("volume %+v", u.vols[5])
	}

	// too small: grown first
	u.vols[3] = &fakeVol{v: Volume{ID: 3, Static: true, Bytes: 4}}
	if res := WriteVolume(u, kernel, "kernel", path, im); !res.Written || u.vols[3].v.Bytes != im.Size {
		t.Errorf("%+v, %d bytes", res, u.vols[3].v.Bytes)
	}

	// fails once: retried
	u.failUpdate = 1
	if res := WriteVolume(u, kernel, "kernel", path, im); !res.Written || res.Retries != 1 {
		t.Errorf("%+v", res)
	}

	// doesn't read back twice: emptied
	u.corrupt = 2
	if res := WriteVolume(u, kernel, "kernel", path, im); res.Written || res.Error == "" || len(u.vols[3].data) != 0 {
		t.Errorf("%+v, %d bytes left", res, len(u.vols[3].data))
	}
}

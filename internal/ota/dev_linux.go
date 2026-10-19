package ota

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctls from linux/mtd/mtd-abi.h and linux/mtd/ubi-user.h.
const (
	memGetInfo     = 0x80204d01 // _IOR('M', 1, struct mtd_info_user)
	memErase       = 0x40084d02 // _IOW('M', 2, struct erase_info_user)
	memGetBadBlock = 0x40084d0b // _IOW('M', 11, __kernel_loff_t)
	memSetBadBlock = 0x40084d0c // _IOW('M', 12, __kernel_loff_t)
	mtdFileMode    = 0x4d13     // _IO('M', 19)
	memWrite       = 0xc0304d18 // _IOWR('M', 24, struct mtd_write_req)
	ubiMkVol       = 0x40986f00 // _IOW('o', 0, struct ubi_mkvol_req)
	ubiRsVol       = 0x400c6f02 // _IOW('o', 2, struct ubi_rsvol_req)
	ubiVolUp       = 0x40084f00 // _IOW('O', 0, __s64)

	mtdFileModeNormal = 0
	mtdFileModeRaw    = 3
	mtdOpsRaw         = 2
	ubiDynamic        = 3
	ubiStatic         = 4
)

type mtdInfo struct {
	Type      uint8
	_         [3]byte
	Flags     uint32
	Size      uint32
	EraseSize uint32
	WriteSize uint32
	OOBSize   uint32
	_         uint64
}

type eraseInfo struct{ Start, Length uint32 }

type mtdWriteReq struct {
	Start, Len, OOBLen, Data, OOB uint64
	Mode                          uint8
	_                             [7]byte
}

type ubiMkVolReq struct {
	VolID     int32
	Alignment int32
	Bytes     int64
	VolType   int8
	Flags     uint8
	NameLen   int16
	_         [4]byte
	Name      [128]byte
}

type ubiRsVolReq struct {
	Bytes int64
	VolID int32
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) (uintptr, error) {
	return ioctlInt(fd, req, uintptr(arg))
}

func ioctlInt(fd uintptr, req uintptr, arg uintptr) (uintptr, error) {
	r, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, arg)
	if errno != 0 {
		return 0, errno
	}
	return r, nil
}

// MTD is an MTD partition, opened by name.
type MTD struct {
	f    *os.File
	info mtdInfo
}

// OpenMTD opens the MTD partition called name in /proc/mtd.
func OpenMTD(name string) (*MTD, error) {
	f, err := os.Open("/proc/mtd")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		// mtd0: 00100000 00020000 "idbloader"
		dev, rest, ok := strings.Cut(s.Text(), ":")
		if ok && strings.HasSuffix(rest, `"`+name+`"`) {
			return openMTD("/dev/" + dev)
		}
	}
	return nil, fmt.Errorf("no MTD partition %q", name)
}

func openMTD(path string) (*MTD, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	m := &MTD{f: f}
	if _, err := ioctl(f.Fd(), memGetInfo, unsafe.Pointer(&m.info)); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func (m *MTD) Close() error                             { return m.f.Close() }
func (m *MTD) Size() int64                              { return int64(m.info.Size) }
func (m *MTD) EraseSize() int64                         { return int64(m.info.EraseSize) }
func (m *MTD) PageSize() int                            { return int(m.info.WriteSize) }
func (m *MTD) OOBSize() int                             { return int(m.info.OOBSize) }
func (m *MTD) ReadAt(b []byte, off int64) (int, error)  { return m.f.ReadAt(b, off) }
func (m *MTD) WriteAt(b []byte, off int64) (int, error) { return m.f.WriteAt(b, off) }

func (m *MTD) Erase(off int64) error {
	e := eraseInfo{uint32(off), m.info.EraseSize}
	_, err := ioctl(m.f.Fd(), memErase, unsafe.Pointer(&e))
	return err
}

func (m *MTD) IsBad(off int64) (bool, error) {
	r, err := ioctl(m.f.Fd(), memGetBadBlock, unsafe.Pointer(&off))
	return r > 0, err
}

func (m *MTD) MarkBad(off int64) error {
	_, err := ioctl(m.f.Fd(), memSetBadBlock, unsafe.Pointer(&off))
	return err
}

// ReadRaw reads without ECC: a header page fails ECC by design.
func (m *MTD) ReadRaw(b []byte, off int64) error {
	if _, err := ioctlInt(m.f.Fd(), mtdFileMode, mtdFileModeRaw); err != nil {
		return err
	}
	defer ioctlInt(m.f.Fd(), mtdFileMode, mtdFileModeNormal)
	_, err := m.f.ReadAt(b, off)
	return err
}

func (m *MTD) WritePageRaw(off int64, data, oob []byte) error {
	req := mtdWriteReq{
		Start: uint64(off), Len: uint64(len(data)), OOBLen: uint64(len(oob)),
		Data: uint64(uintptr(unsafe.Pointer(&data[0]))), OOB: uint64(uintptr(unsafe.Pointer(&oob[0]))),
		Mode: mtdOpsRaw,
	}
	_, err := ioctl(m.f.Fd(), memWrite, unsafe.Pointer(&req))
	runtime.KeepAlive(data)
	runtime.KeepAlive(oob)
	return err
}

// UBI0 is UBI device 0, through /dev/ubi0 and sysfs.
type UBI0 struct{}

func (UBI0) node(id int) string { return fmt.Sprintf("/dev/ubi0_%d", id) }

func (UBI0) Volume(id int) (int64, bool, error) {
	dir := fmt.Sprintf("/sys/class/ubi/ubi0_%d/", id)
	ebs, err := readInt(dir + "reserved_ebs")
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	size, err := readInt(dir + "usable_eb_size")
	return ebs * size, true, err
}

func readInt(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}

func (u UBI0) Create(v Volume) error {
	req := ubiMkVolReq{VolID: int32(v.ID), Alignment: 1, Bytes: v.Bytes, VolType: ubiDynamic, NameLen: int16(len(v.Name))}
	if v.Static {
		req.VolType = ubiStatic
	}
	copy(req.Name[:], v.Name)
	f, err := os.OpenFile("/dev/ubi0", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := ioctl(f.Fd(), ubiMkVol, unsafe.Pointer(&req)); err != nil {
		return fmt.Errorf("creating %s: %w", v.Name, err)
	}
	// devtmpfs adds the node
	for range 50 {
		if _, err := os.Stat(u.node(v.ID)); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("no %s", u.node(v.ID))
}

func (UBI0) Resize(id int, bytes int64) error {
	f, err := os.OpenFile("/dev/ubi0", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	req := ubiRsVolReq{Bytes: bytes, VolID: int32(id)}
	_, err = ioctl(f.Fd(), ubiRsVol, unsafe.Pointer(&req))
	return err
}

func (u UBI0) Update(id int, r io.Reader, size int64) error {
	f, err := os.OpenFile(u.node(id), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if _, err := ioctl(f.Fd(), ubiVolUp, unsafe.Pointer(&size)); err != nil {
		f.Close()
		return err
	}
	if size > 0 {
		if _, err := io.CopyN(f, r, size); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func (u UBI0) Open(id int) (io.ReadCloser, error) { return os.Open(u.node(id)) }

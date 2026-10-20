// Package drm drives a display through Linux KMS with a single dumb buffer:
// find the connected connector, allocate an XRGB8888 buffer, map it and show
// it. Only the few ioctls needed for that are implemented.
package drm

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// ioctl numbers: _IOWR('d', nr, size) / _IO('d', nr)
func iowr(nr, size uintptr) uintptr { return 3<<30 | size<<16 | 'd'<<8 | nr }

var (
	ioctlSetMaster   = uintptr('d'<<8 | 0x1e)
	ioctlGetRes      = iowr(0xa0, unsafe.Sizeof(cardRes{}))
	ioctlSetCrtc     = iowr(0xa2, unsafe.Sizeof(crtc{}))
	ioctlGetEncoder  = iowr(0xa6, unsafe.Sizeof(encoder{}))
	ioctlGetConn     = iowr(0xa7, unsafe.Sizeof(connector{}))
	ioctlAddFB       = iowr(0xae, unsafe.Sizeof(fbCmd{}))
	ioctlRmFB        = iowr(0xaf, 4)
	ioctlCreateDumb  = iowr(0xb2, unsafe.Sizeof(createDumb{}))
	ioctlMapDumb     = iowr(0xb3, unsafe.Sizeof(mapDumb{}))
	ioctlDestroyDumb = iowr(0xb4, 4)
)

// struct drm_mode_card_res
type cardRes struct {
	fbs, crtcs, connectors, encoders         uint64
	nFbs, nCrtcs, nConnectors, nEncoders     uint32
	minWidth, maxWidth, minHeight, maxHeight uint32
}

// struct drm_mode_modeinfo
type modeInfo struct {
	Clock                                         uint32
	HDisplay, HSyncStart, HSyncEnd, HTotal, HSkew uint16
	VDisplay, VSyncStart, VSyncEnd, VTotal, VScan uint16
	VRefresh, Flags, Type                         uint32
	Name                                          [32]byte
}

// struct drm_mode_crtc
type crtc struct {
	setConnectors               uint64
	nConnectors, id, fbID, x, y uint32
	gammaSize, modeValid        uint32
	mode                        modeInfo
}

// struct drm_mode_get_encoder
type encoder struct {
	id, typ, crtcID, possibleCrtcs, possibleClones uint32
}

// struct drm_mode_get_connector
type connector struct {
	encoders, modes, props, propValues   uint64
	nModes, nProps, nEncoders, encoderID uint32
	id, typ, typID, connection           uint32
	mmWidth, mmHeight, subpixel, pad     uint32
}

// struct drm_mode_fb_cmd
type fbCmd struct {
	id, width, height, pitch, bpp, depth, handle uint32
}

// struct drm_mode_create_dumb
type createDumb struct {
	height, width, bpp, flags, handle, pitch uint32
	size                                     uint64
}

// struct drm_mode_map_dumb
type mapDumb struct {
	handle, pad uint32
	offset      uint64
}

// addr returns the address of the first element of s for an ioctl struct
// field. The pinner keeps it on the heap and in place until it is unpinned,
// so the kernel never sees a moved or freed buffer.
func addr[T any](p *runtime.Pinner, s []T) uint64 {
	if len(s) == 0 {
		return 0
	}
	p.Pin(&s[0])
	return uint64(uintptr(unsafe.Pointer(&s[0])))
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// Display is a mode set on a connector, showing one mapped dumb buffer.
type Display struct {
	// Width and Height are the mode size in pixels; Stride is the buffer
	// pitch in pixels.
	Width, Height, Stride int
	// Pixels is the mapped buffer, XRGB8888 (0x00RRGGBB per pixel).
	Pixels []uint32

	f      *os.File
	mem    []byte
	handle uint32
	fbID   uint32
	connID uint32
	crtcID uint32
	mode   modeInfo
}

// Open sets up the first connected connector of card at its preferred mode.
func Open(card string) (*Display, error) {
	f, err := os.OpenFile(card, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	d := &Display{f: f}
	if err := d.setup(); err != nil {
		d.Close()
		return nil, fmt.Errorf("%s: %w", card, err)
	}
	return d, nil
}

func (d *Display) setup() error {
	fd := d.f.Fd()
	_ = ioctl(fd, ioctlSetMaster, nil) // the first opener is master anyway

	var res cardRes
	if err := ioctl(fd, ioctlGetRes, unsafe.Pointer(&res)); err != nil {
		return fmt.Errorf("get resources: %w", err)
	}
	conns := make([]uint32, res.nConnectors)
	crtcs := make([]uint32, res.nCrtcs)
	var pin runtime.Pinner
	res = cardRes{nConnectors: res.nConnectors, nCrtcs: res.nCrtcs,
		connectors: addr(&pin, conns), crtcs: addr(&pin, crtcs)}
	err := ioctl(fd, ioctlGetRes, unsafe.Pointer(&res))
	pin.Unpin()
	if err != nil {
		return fmt.Errorf("get resources: %w", err)
	}

	for _, id := range conns {
		conn, mode, err := d.connector(id)
		if err != nil || conn.connection != 1 { // 1 = connected
			continue
		}
		crtcID, err := d.pickCrtc(conn, crtcs)
		if err != nil {
			return err
		}
		return d.show(conn.id, crtcID, mode)
	}
	return errors.New("no connected display")
}

// connector returns a connector and its preferred (or first) mode.
func (d *Display) connector(id uint32) (connector, modeInfo, error) {
	fd := d.f.Fd()
	c := connector{id: id}
	if err := ioctl(fd, ioctlGetConn, unsafe.Pointer(&c)); err != nil {
		return c, modeInfo{}, err
	}
	if c.nModes == 0 {
		return c, modeInfo{}, errors.New("no modes")
	}
	modes := make([]modeInfo, c.nModes)
	encs := make([]uint32, max(c.nEncoders, 1))
	var pin runtime.Pinner
	c = connector{id: id, nModes: c.nModes, modes: addr(&pin, modes),
		nEncoders: c.nEncoders, encoders: addr(&pin, encs)}
	err := ioctl(fd, ioctlGetConn, unsafe.Pointer(&c))
	pin.Unpin()
	if err != nil {
		return c, modeInfo{}, err
	}
	// Before the first mode set no encoder is attached: use the first possible one.
	if c.encoderID == 0 && c.nEncoders > 0 {
		c.encoderID = encs[0]
	}
	for _, m := range modes {
		if m.Type&(1<<3) != 0 { // DRM_MODE_TYPE_PREFERRED
			return c, m, nil
		}
	}
	return c, modes[0], nil
}

// pickCrtc returns the CRTC behind the connector's encoder, or the first one
// the encoder can drive.
func (d *Display) pickCrtc(c connector, crtcs []uint32) (uint32, error) {
	e := encoder{id: c.encoderID}
	if c.encoderID == 0 {
		return 0, errors.New("connector has no encoder")
	}
	if err := ioctl(d.f.Fd(), ioctlGetEncoder, unsafe.Pointer(&e)); err != nil {
		return 0, fmt.Errorf("get encoder: %w", err)
	}
	if e.crtcID != 0 {
		return e.crtcID, nil
	}
	for i, id := range crtcs {
		if e.possibleCrtcs&(1<<i) != 0 {
			return id, nil
		}
	}
	return 0, errors.New("no usable CRTC")
}

// show creates and maps a dumb buffer for mode and scans it out.
func (d *Display) show(connID, crtcID uint32, mode modeInfo) error {
	fd := d.f.Fd()
	cd := createDumb{width: uint32(mode.HDisplay), height: uint32(mode.VDisplay), bpp: 32}
	if err := ioctl(fd, ioctlCreateDumb, unsafe.Pointer(&cd)); err != nil {
		return fmt.Errorf("create dumb buffer: %w", err)
	}
	d.handle = cd.handle

	fb := fbCmd{width: cd.width, height: cd.height, pitch: cd.pitch, bpp: 32, depth: 24, handle: cd.handle}
	if err := ioctl(fd, ioctlAddFB, unsafe.Pointer(&fb)); err != nil {
		return fmt.Errorf("add framebuffer: %w", err)
	}
	d.fbID = fb.id

	md := mapDumb{handle: cd.handle}
	if err := ioctl(fd, ioctlMapDumb, unsafe.Pointer(&md)); err != nil {
		return fmt.Errorf("map dumb buffer: %w", err)
	}
	mem, err := syscall.Mmap(int(fd), int64(md.offset), int(cd.size),
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("mmap: %w", err)
	}
	d.mem = mem
	d.Pixels = unsafe.Slice((*uint32)(unsafe.Pointer(&mem[0])), len(mem)/4)
	d.Width, d.Height, d.Stride = int(cd.width), int(cd.height), int(cd.pitch/4)
	clear(d.Pixels)
	d.connID, d.crtcID, d.mode = connID, crtcID, mode
	return d.On()
}

// On scans the buffer out: at start, and after Off.
func (d *Display) On() error {
	var pin runtime.Pinner
	defer pin.Unpin()
	sc := crtc{
		setConnectors: addr(&pin, []uint32{d.connID}),
		nConnectors:   1,
		id:            d.crtcID,
		fbID:          d.fbID,
		modeValid:     1,
		mode:          d.mode,
	}
	if err := ioctl(d.f.Fd(), ioctlSetCrtc, unsafe.Pointer(&sc)); err != nil {
		return fmt.Errorf("set CRTC: %w", err)
	}
	return nil
}

// Off stops the CRTC, which powers the panel down. The buffer stays, and
// On shows it again.
func (d *Display) Off() error {
	sc := crtc{id: d.crtcID}
	if err := ioctl(d.f.Fd(), ioctlSetCrtc, unsafe.Pointer(&sc)); err != nil {
		return fmt.Errorf("disable CRTC: %w", err)
	}
	return nil
}

// Close releases the buffer and the device; the display keeps its last image
// until another program sets a mode.
func (d *Display) Close() error {
	fd := d.f.Fd()
	if d.mem != nil {
		syscall.Munmap(d.mem)
		d.mem, d.Pixels = nil, nil
	}
	if d.fbID != 0 {
		id := d.fbID
		ioctl(fd, ioctlRmFB, unsafe.Pointer(&id))
	}
	if d.handle != 0 {
		h := d.handle
		ioctl(fd, ioctlDestroyDumb, unsafe.Pointer(&h))
	}
	return d.f.Close()
}

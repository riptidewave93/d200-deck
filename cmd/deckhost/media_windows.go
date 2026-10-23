package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/riptidewave93/d200-deck/deck"
)

// Media players on Windows: the system media transport controls (WinRT
// Windows.Media.Control), called through their COM vtables. Method numbers
// count IInspectable's 6 first; the order is the interfaces' in the Windows
// metadata.

var (
	combase                   = windows.NewLazySystemDLL("combase.dll")
	roInitialize              = combase.NewProc("RoInitialize")
	roGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	windowsCreateString       = combase.NewProc("WindowsCreateString")
	windowsDeleteString       = combase.NewProc("WindowsDeleteString")
	windowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")
	// turns a WinRT stream into a COM IStream, which is simpler to read
	createStreamOverRandomAccessStream = windows.NewLazySystemDLL("shcore.dll").NewProc("CreateStreamOverRandomAccessStream")

	// IGlobalSystemMediaTransportControlsSessionManagerStatics
	iidManagerStatics = windows.GUID{Data1: 0x2050c4ee, Data2: 0x11a0, Data3: 0x57de, Data4: [8]byte{0xae, 0xd7, 0xc9, 0x7c, 0x70, 0x33, 0x82, 0x45}}
	iidAsyncInfo      = windows.GUID{Data1: 0x00000036, Data2: 0, Data3: 0, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIStream        = windows.GUID{Data1: 0x0000000c, Data2: 0, Data3: 0, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const managerClass = "Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager"

// vtable method numbers
const (
	mRelease = 2

	mRequestAsync = 6 // ManagerStatics

	mGetCurrentSession = 6 // Manager
	mGetSessions       = 7

	mSourceAppUserModelID   = 6 // Session
	mTryGetMediaProperties  = 7
	mGetPlaybackInfo        = 9
	mTryStop                = 12
	mTrySkipNext            = 16
	mTrySkipPrevious        = 17
	mTryTogglePlayPause     = 20
	mTryChangeAutoRepeat    = 21
	mTryChangeShuffleActive = 23

	mTitle     = 6 // MediaProperties
	mArtist    = 9
	mThumbnail = 15

	mOpenReadAsync = 6 // IRandomAccessStreamReference

	mStreamRead = 3 // IStream (ISequentialStream::Read)

	mPlaybackStatus  = 7 // PlaybackInfo
	mAutoRepeatMode  = 9
	mIsShuffleActive = 11

	mVectorGetAt = 6 // IVectorView
	mVectorSize  = 7

	mReferenceValue = 6 // IReference

	mAsyncStatus = 7 // IAsyncInfo
	mGetResults  = 8 // IAsyncOperation
)

const (
	statusPlaying  = 4 // GlobalSystemMediaTransportControlsSessionPlaybackStatus
	repeatNone     = 0 // MediaPlaybackAutoRepeatMode
	repeatList     = 2
	asyncStarted   = 0
	asyncCompleted = 1
)

// comCall calls method m of COM object obj.
//
//go:uintptrescapes
func comCall(obj unsafe.Pointer, m int, args ...uintptr) error {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, m*int(unsafe.Sizeof(uintptr(0)))))
	hr, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	if int32(hr) < 0 {
		return hresult(hr)
	}
	return nil
}

// hresult is a failed COM call's HRESULT.
type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("HRESULT %#08x", uint32(h)) }

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		comCall(obj, mRelease)
	}
}

// getObj calls method m, which returns an object.
func getObj(obj unsafe.Pointer, m int) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	err := comCall(obj, m, uintptr(unsafe.Pointer(&out)))
	return out, err
}

// await waits for async operation op, then releases it and returns its
// result (an object, or nil for others).
func await(op unsafe.Pointer, object bool) (unsafe.Pointer, error) {
	defer comRelease(op)
	var info unsafe.Pointer
	if err := comCall(op, 0, uintptr(unsafe.Pointer(&iidAsyncInfo)), uintptr(unsafe.Pointer(&info))); err != nil {
		return nil, err
	}
	defer comRelease(info)
	var status int32
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if err := comCall(info, mAsyncStatus, uintptr(unsafe.Pointer(&status))); err != nil {
			return nil, err
		}
		if status != asyncStarted || time.Now().After(deadline) {
			break
		}
	}
	if status != asyncCompleted {
		return nil, fmt.Errorf("async status %d", status)
	}
	if !object {
		return nil, nil
	}
	return getObj(op, mGetResults)
}

func hstring(s string) (uintptr, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	if r, _, _ := windowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h))); int32(r) < 0 {
		return 0, fmt.Errorf("WindowsCreateString: %#08x", uint32(r))
	}
	return h, nil
}

func goString(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := windowsGetStringRawBuffer.Call(h, uintptr(unsafe.Pointer(&n)))
	if p == 0 {
		return ""
	}
	return windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&p))) // p is WinRT's, not Go's
}

// smtc runs every WinRT call on one OS thread, which joins the
// multithreaded apartment.
type smtc struct {
	calls chan func()
	mgr   unsafe.Pointer
}

func openMedia() (mediaBackend, error) {
	if combase.Load() != nil {
		return nil, errors.ErrUnsupported
	}
	s := &smtc{calls: make(chan func())}
	errc := make(chan error)
	go s.loop(errc)
	if err := <-errc; err != nil {
		return nil, err
	}
	return s, nil
}

func (s *smtc) loop(errc chan<- error) {
	runtime.LockOSThread() // never unlocked: the thread ends with the loop
	roInitialize.Call(1)   // RO_INIT_MULTITHREADED
	err := s.start()
	errc <- err
	if err != nil {
		return
	}
	for f := range s.calls {
		f()
	}
	comRelease(s.mgr)
}

func (s *smtc) start() error {
	class, err := hstring(managerClass)
	if err != nil {
		return err
	}
	defer windowsDeleteString.Call(class)
	var statics unsafe.Pointer
	if r, _, _ := roGetActivationFactory.Call(class, uintptr(unsafe.Pointer(&iidManagerStatics)), uintptr(unsafe.Pointer(&statics))); int32(r) < 0 {
		return fmt.Errorf("media controls: %#08x", uint32(r)) // before Windows 10 1809
	}
	defer comRelease(statics)
	op, err := getObj(statics, mRequestAsync)
	if err != nil {
		return err
	}
	s.mgr, err = await(op, true)
	return err
}

// do runs f on the WinRT thread.
func (s *smtc) do(f func() error) error {
	done := make(chan error, 1)
	s.calls <- func() { done <- f() }
	return <-done
}

func (s *smtc) close() { close(s.calls) }

// sessions calls f with each media session and its app's ID, the system's
// current session first.
func (s *smtc) sessions(f func(sess unsafe.Pointer, app string) error) error {
	current := ""
	if cur, err := getObj(s.mgr, mGetCurrentSession); err == nil && cur != nil {
		current = sessionApp(cur)
		comRelease(cur)
	}
	list, err := getObj(s.mgr, mGetSessions)
	if err != nil {
		return err
	}
	defer comRelease(list)
	var n uint32
	if err := comCall(list, mVectorSize, uintptr(unsafe.Pointer(&n))); err != nil {
		return err
	}
	type entry struct {
		sess unsafe.Pointer
		app  string
	}
	var all []entry
	for i := range n {
		var sess unsafe.Pointer
		if comCall(list, mVectorGetAt, uintptr(i), uintptr(unsafe.Pointer(&sess))) != nil || sess == nil {
			continue
		}
		e := entry{sess, sessionApp(sess)}
		if e.app == current {
			all = append([]entry{e}, all...)
		} else {
			all = append(all, e)
		}
	}
	defer func() {
		for _, e := range all {
			comRelease(e.sess)
		}
	}()
	for _, e := range all {
		if err := f(e.sess, e.app); err != nil {
			return err
		}
	}
	return nil
}

func sessionApp(sess unsafe.Pointer) string { return getString(sess, mSourceAppUserModelID) }

// getString calls method m, which returns a string.
func getString(obj unsafe.Pointer, m int) string {
	var h uintptr
	if comCall(obj, m, uintptr(unsafe.Pointer(&h))) != nil {
		return ""
	}
	defer windowsDeleteString.Call(h)
	return goString(h)
}

// mediaProperties are the song of session sess (released by the caller).
func mediaProperties(sess unsafe.Pointer) (unsafe.Pointer, error) {
	op, err := getObj(sess, mTryGetMediaProperties)
	if err != nil {
		return nil, err
	}
	return await(op, true)
}

// thumbnail reads the album art in media properties props; nil if none.
func thumbnail(props unsafe.Pointer) ([]byte, error) {
	ref, err := getObj(props, mThumbnail)
	if err != nil || ref == nil {
		return nil, err
	}
	defer comRelease(ref)
	op, err := getObj(ref, mOpenReadAsync)
	if err != nil {
		return nil, err
	}
	stream, err := await(op, true)
	if err != nil {
		return nil, err
	}
	defer comRelease(stream)
	var is unsafe.Pointer
	if r, _, _ := createStreamOverRandomAccessStream.Call(uintptr(stream), uintptr(unsafe.Pointer(&iidIStream)), uintptr(unsafe.Pointer(&is))); int32(r) < 0 {
		return nil, fmt.Errorf("CreateStreamOverRandomAccessStream: %#08x", uint32(r))
	}
	defer comRelease(is)
	var out []byte
	buf := make([]byte, 64<<10)
	for len(out) <= maxArtFile {
		var n uint32
		if err := comCall(is, mStreamRead, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n))); err != nil {
			return nil, err
		}
		if n == 0 {
			return out, nil
		}
		out = append(out, buf[:n]...)
	}
	return nil, errors.New("art file too big")
}

// appName is the short name of an app's ID: Spotify.exe and
// SpotifyAB.SpotifyMusic_zpdnekdrzrea0!Spotify are both spotify.
func appName(app string) string {
	if i := strings.LastIndexByte(app, '!'); i >= 0 {
		app = app[i+1:]
	}
	app = strings.TrimSuffix(strings.TrimSuffix(app, ".exe"), ".EXE")
	if i := strings.LastIndexByte(app, '.'); i >= 0 {
		app = app[i+1:]
	}
	return shortName(app)
}

func (s *smtc) players() ([]mediaPlayer, error) {
	var ps []mediaPlayer
	err := s.do(func() error {
		return s.sessions(func(sess unsafe.Pointer, app string) error {
			p := mediaPlayer{ID: app, Name: appName(app)}
			if props, err := mediaProperties(sess); err == nil && props != nil {
				p.Title, p.Artist = getString(props, mTitle), getString(props, mArtist)
				comRelease(props)
			}
			info, err := getObj(sess, mGetPlaybackInfo)
			if err != nil || info == nil {
				return nil
			}
			defer comRelease(info)
			var status int32
			if comCall(info, mPlaybackStatus, uintptr(unsafe.Pointer(&status))) == nil {
				p.Playing = status == statusPlaying
			}
			if ref, err := getObj(info, mAutoRepeatMode); err == nil && ref != nil {
				var mode int32
				if comCall(ref, mReferenceValue, uintptr(unsafe.Pointer(&mode))) == nil {
					p.Repeat = mode != repeatNone
				}
				comRelease(ref)
			}
			if ref, err := getObj(info, mIsShuffleActive); err == nil && ref != nil {
				var on uint8
				if comCall(ref, mReferenceValue, uintptr(unsafe.Pointer(&on))) == nil {
					p.Shuffle = on != 0
				}
				comRelease(ref)
			}
			ps = append(ps, p)
			return nil
		})
	})
	return ps, err
}

func (s *smtc) art(p mediaPlayer) ([]byte, error) {
	var img []byte
	err := s.do(func() error {
		return s.sessions(func(sess unsafe.Pointer, app string) error {
			if app != p.ID {
				return nil
			}
			props, err := mediaProperties(sess)
			if err != nil || props == nil {
				return err
			}
			defer comRelease(props)
			img, err = thumbnail(props)
			if err == nil {
				err = errDone
			}
			return err
		})
	})
	if errors.Is(err, errDone) {
		err = nil
	}
	return img, err
}

var errDone = errors.New("done")

func (s *smtc) control(p mediaPlayer, op string) error {
	err := s.do(func() error {
		return s.sessions(func(sess unsafe.Pointer, app string) error {
			if app != p.ID {
				return nil
			}
			var async unsafe.Pointer
			var err error
			switch op {
			case deck.MediaPlayPause:
				async, err = getObj(sess, mTryTogglePlayPause)
			case deck.MediaNext:
				async, err = getObj(sess, mTrySkipNext)
			case deck.MediaPrevious:
				async, err = getObj(sess, mTrySkipPrevious)
			case deck.MediaStop:
				async, err = getObj(sess, mTryStop)
			case deck.MediaShuffle:
				on := uintptr(1)
				if p.Shuffle {
					on = 0
				}
				err = comCall(sess, mTryChangeShuffleActive, on, uintptr(unsafe.Pointer(&async)))
			case deck.MediaRepeat:
				mode := uintptr(repeatList)
				if p.Repeat {
					mode = repeatNone
				}
				err = comCall(sess, mTryChangeAutoRepeat, mode, uintptr(unsafe.Pointer(&async)))
			}
			if err == nil && async != nil {
				_, err = await(async, false)
			}
			if err == nil {
				err = errDone
			}
			return err
		})
	})
	switch {
	case errors.Is(err, errDone):
		return nil
	case err == nil:
		return errors.New("player gone")
	}
	return err
}

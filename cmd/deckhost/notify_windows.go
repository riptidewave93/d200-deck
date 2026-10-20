package main

import (
	"log"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Notifications on Windows: a balloon from a notification area icon of its
// own, which Windows 10 and later show as a toast. The icon goes when the
// balloon does; the tray menu still offers the release page.

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	shellNotifyIcon   = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	registerClassEx   = user32.NewProc("RegisterClassExW")
	createWindowEx    = user32.NewProc("CreateWindowExW")
	destroyWindow     = user32.NewProc("DestroyWindow")
	defWindowProc     = user32.NewProc("DefWindowProcW")
	getMessage        = user32.NewProc("GetMessageW")
	dispatchMessage   = user32.NewProc("DispatchMessageW")
	postQuitMessage   = user32.NewProc("PostQuitMessage")
	loadIcon          = user32.NewProc("LoadIconW")
	notifyClass       sync.Once
	notifyClassName   = windows.StringToUTF16Ptr("deckhost-notify")
	notifyMu          sync.Mutex // one notification at a time
	notifyURL         string
	notifyWindowProcC uintptr
)

const (
	nimAdd, nimDelete              = 0, 2
	nifMessage, nifIcon, nifTip    = 0x1, 0x2, 0x4
	nifInfo                        = 0x10
	niifInfo                       = 0x1
	wmApp                          = 0x8000
	wmLButtonUp                    = 0x202
	ninBalloonHide, ninBalloonTime = 0x403, 0x404
	ninBalloonClick                = 0x405
	idiInformation                 = 32516
)

// notifyIconData is NOTIFYICONDATAW.
type notifyIconData struct {
	Size            uint32
	Wnd             windows.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        windows.GUID
	BalloonIcon     windows.Handle
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Wnd     windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

// notify shows a notification that opens url when clicked.
func notify(title, body, url string) {
	go func() {
		if err := showNotification(title, body, url); err != nil {
			log.Printf("notification: %v", err)
		}
	}()
}

func showNotification(title, body, url string) error {
	notifyMu.Lock()
	defer notifyMu.Unlock()
	runtime.LockOSThread() // the window's messages come to this thread
	defer runtime.UnlockOSThread()
	notifyURL = url

	var err error
	notifyClass.Do(func() {
		notifyWindowProcC = windows.NewCallback(notifyWindowProc)
		wc := wndClassEx{WndProc: notifyWindowProcC, ClassName: notifyClassName}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, e := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			err = e
		}
	})
	if err != nil {
		return err
	}
	wnd, _, e := createWindowEx.Call(0, uintptr(unsafe.Pointer(notifyClassName)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if wnd == 0 {
		return e
	}
	defer destroyWindow.Call(wnd)

	icon, _, _ := loadIcon.Call(0, idiInformation)
	nid := notifyIconData{Wnd: windows.Handle(wnd), ID: 1, Flags: nifMessage | nifIcon | nifTip | nifInfo,
		CallbackMessage: wmApp + 1, Icon: windows.Handle(icon), InfoFlags: niifInfo}
	nid.Size = uint32(unsafe.Sizeof(nid))
	copyUTF16(nid.Tip[:], "deckhost")
	copyUTF16(nid.InfoTitle[:], title)
	copyUTF16(nid.Info[:], body)
	if r, _, e := shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return e
	}
	defer shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))

	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return nil
		}
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func notifyWindowProc(wnd, message, wParam, lParam uintptr) uintptr {
	if message == wmApp+1 {
		switch lParam & 0xffff {
		case ninBalloonClick, wmLButtonUp:
			if err := openURL(notifyURL); err != nil {
				log.Printf("notification: %v", err)
			}
			postQuitMessage.Call(0)
		case ninBalloonHide, ninBalloonTime:
			postQuitMessage.Call(0)
		}
		return 0
	}
	r, _, _ := defWindowProc.Call(wnd, message, wParam, lParam)
	return r
}

// copyUTF16 puts s in dst, cut to fit with its NUL.
func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

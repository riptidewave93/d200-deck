package main

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The mic on Windows: the default recording devices (the default and the
// default communications one, often the same), through Core Audio's
// IAudioEndpointVolume. Each call looks them up again, so it follows
// changes of default device.

var (
	coCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")

	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xbcde0395, Data2: 0xe52f, Data3: 0x467c, Data4: [8]byte{0x8e, 0x3d, 0xc4, 0x57, 0x92, 0x91, 0x69, 0x2e}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xa95664d2, Data2: 0x9614, Data3: 0x4f35, Data4: [8]byte{0xa7, 0x46, 0xde, 0x8d, 0xb6, 0x36, 0x17, 0xe6}}
	iidIAudioEndpointVolume = windows.GUID{Data1: 0x5cdf2c82, Data2: 0x841e, Data3: 0x4546, Data4: [8]byte{0x97, 0x22, 0x0c, 0xf7, 0x40, 0x78, 0x22, 0x9a}}
)

const (
	mGetDefaultAudioEndpoint = 4  // IMMDeviceEnumerator
	mActivate                = 3  // IMMDevice
	mSetMute                 = 14 // IAudioEndpointVolume
	mGetMute                 = 15

	eCapture        = 1
	eConsole        = 0
	eCommunications = 2
	clsctxAll       = 0x17

	sFalse          = 1          // COM was already set up on the thread
	rpcEChangedMode = 0x80010106 // ... single-threaded, which works too
	eNotFound       = 0x80070490 // HRESULT_FROM_WIN32(ERROR_NOT_FOUND)
)

type coreAudioMic struct{}

func openMic() (micBackend, error) {
	if coCreateInstance.Find() != nil {
		return nil, errors.ErrUnsupported
	}
	return coreAudioMic{}, nil
}

func (coreAudioMic) close() {}

// endpoints calls f with each default recording device's volume control.
func endpoints(f func(vol unsafe.Pointer) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var hr hresult
	if errno, ok := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED).(syscall.Errno); ok {
		hr = hresult(errno)
	}
	switch hr {
	case 0, sFalse:
		defer windows.CoUninitialize()
	case rpcEChangedMode:
	default:
		return hr
	}
	var enum unsafe.Pointer
	if r, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum))); int32(r) < 0 {
		return hresult(r)
	}
	defer comRelease(enum)
	for _, role := range []uintptr{eConsole, eCommunications} {
		var dev unsafe.Pointer
		if err := comCall(enum, mGetDefaultAudioEndpoint, eCapture, role, uintptr(unsafe.Pointer(&dev))); err != nil {
			if errors.Is(err, hresult(eNotFound)) {
				if role == eCommunications {
					continue
				}
				return errNoMic
			}
			return err
		}
		var vol unsafe.Pointer
		err := comCall(dev, mActivate, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&vol)))
		comRelease(dev)
		if err != nil {
			return err
		}
		err = f(vol)
		comRelease(vol)
		if err != nil {
			return err
		}
	}
	return nil
}

func (coreAudioMic) muted() (bool, error) {
	all := true
	err := endpoints(func(vol unsafe.Pointer) error {
		var m int32
		err := comCall(vol, mGetMute, uintptr(unsafe.Pointer(&m)))
		all = all && m != 0
		return err
	})
	return all, err
}

func (coreAudioMic) setMuted(on bool) error {
	var m uintptr
	if on {
		m = 1
	}
	return endpoints(func(vol unsafe.Pointer) error { return comCall(vol, mSetMute, m, 0) })
}

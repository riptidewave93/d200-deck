package main

/*
#cgo LDFLAGS: -framework CoreAudio
#include <CoreAudio/CoreAudio.h>

static AudioObjectID defaultInput(void) {
	AudioObjectPropertyAddress a = {kAudioHardwarePropertyDefaultInputDevice, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	AudioObjectID dev = kAudioObjectUnknown;
	UInt32 size = sizeof(dev);
	AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &size, &dev);
	return dev;
}

// noRunLoop has CoreAudio send its notifications from a thread of its own.
// It tracks the default device with them, and the daemon has no run loop.
static void noRunLoop(void) {
	AudioObjectPropertyAddress a = {kAudioHardwarePropertyRunLoop, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	CFRunLoopRef rl = NULL;
	AudioObjectSetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, sizeof(rl), &rl);
}

static int canSet(AudioObjectID dev, AudioObjectPropertySelector sel, UInt32 el) {
	AudioObjectPropertyAddress a = {sel, kAudioDevicePropertyScopeInput, el};
	Boolean ok = false;
	return AudioObjectHasProperty(dev, &a) && AudioObjectIsPropertySettable(dev, &a, &ok) == noErr && ok;
}

static OSStatus get(AudioObjectID dev, AudioObjectPropertySelector sel, UInt32 el, void *v, UInt32 size) {
	AudioObjectPropertyAddress a = {sel, kAudioDevicePropertyScopeInput, el};
	return AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, v);
}

static OSStatus set(AudioObjectID dev, AudioObjectPropertySelector sel, UInt32 el, const void *v, UInt32 size) {
	AudioObjectPropertyAddress a = {sel, kAudioDevicePropertyScopeInput, el};
	return AudioObjectSetPropertyData(dev, &a, 0, NULL, size, v);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"unsafe"
)

// The mic on macOS: the default input device, through CoreAudio. Devices
// without a mute control (some built-in mics) are muted by turning their
// input volume to 0, and unmuted by turning it back.

type coreAudioMic struct {
	mu    sync.Mutex
	saved map[C.AudioObjectID][]float32 // volumes before muting, by device
}

var notifyOnce sync.Once

func openMic() (micBackend, error) {
	notifyOnce.Do(func() { C.noRunLoop() })
	return &coreAudioMic{saved: map[C.AudioObjectID][]float32{}}, nil
}

func (m *coreAudioMic) close() {}

// micDevice is the default input device and the elements its volume is set
// on (nil if it has a mute control).
func micDevice() (C.AudioObjectID, []C.UInt32, error) {
	dev := C.defaultInput()
	if dev == C.kAudioObjectUnknown {
		return 0, nil, errNoMic
	}
	if C.canSet(dev, C.kAudioDevicePropertyMute, C.kAudioObjectPropertyElementMain) != 0 {
		return dev, nil, nil
	}
	if C.canSet(dev, C.kAudioDevicePropertyVolumeScalar, C.kAudioObjectPropertyElementMain) != 0 {
		return dev, []C.UInt32{C.kAudioObjectPropertyElementMain}, nil
	}
	var els []C.UInt32
	for ch := C.UInt32(1); ch <= 8; ch++ {
		if C.canSet(dev, C.kAudioDevicePropertyVolumeScalar, ch) != 0 {
			els = append(els, ch)
		}
	}
	if els == nil {
		return 0, nil, errors.New("the microphone has no mute or volume control")
	}
	return dev, els, nil
}

func caStatus(s C.OSStatus) error {
	if s != 0 {
		return fmt.Errorf("CoreAudio error %d", int32(s))
	}
	return nil
}

func (m *coreAudioMic) volumes(dev C.AudioObjectID, els []C.UInt32) ([]float32, error) {
	vs := make([]float32, len(els))
	for i, el := range els {
		var v C.Float32
		if err := caStatus(C.get(dev, C.kAudioDevicePropertyVolumeScalar, el, unsafe.Pointer(&v), C.UInt32(unsafe.Sizeof(v)))); err != nil {
			return nil, err
		}
		vs[i] = float32(v)
	}
	return vs, nil
}

func (m *coreAudioMic) muted() (bool, error) {
	dev, els, err := micDevice()
	if err != nil {
		return false, err
	}
	if els == nil {
		var mute C.UInt32
		err := caStatus(C.get(dev, C.kAudioDevicePropertyMute, C.kAudioObjectPropertyElementMain, unsafe.Pointer(&mute), C.UInt32(unsafe.Sizeof(mute))))
		return mute != 0, err
	}
	vs, err := m.volumes(dev, els)
	for _, v := range vs {
		if v > 0 {
			return false, err
		}
	}
	return true, err
}

func (m *coreAudioMic) setMuted(on bool) error {
	dev, els, err := micDevice()
	if err != nil {
		return err
	}
	if els == nil {
		mute := C.UInt32(0)
		if on {
			mute = 1
		}
		return caStatus(C.set(dev, C.kAudioDevicePropertyMute, C.kAudioObjectPropertyElementMain, unsafe.Pointer(&mute), C.UInt32(unsafe.Sizeof(mute))))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	vs := make([]float32, len(els)) // muted: all 0
	if on {
		if now, err := m.volumes(dev, els); err == nil && slices.Max(now) > 0 {
			m.saved[dev] = now
		}
	} else if saved := m.saved[dev]; len(saved) == len(els) {
		vs = saved
	} else {
		for i := range vs {
			vs[i] = 0.5 // deckhost restarted while muted, so the level is lost
		}
	}
	for i, el := range els {
		v := C.Float32(vs[i])
		if err := caStatus(C.set(dev, C.kAudioDevicePropertyVolumeScalar, el, unsafe.Pointer(&v), C.UInt32(unsafe.Sizeof(v)))); err != nil {
			return err
		}
	}
	return nil
}

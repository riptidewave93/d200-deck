#include <stdint.h>
#include <stdlib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/hid/IOHIDDevice.h>
#include <IOKit/hid/IOHIDKeys.h>
#include "_cgo_export.h"

// An open device, scheduled on the run loop of the thread running hid_run.
typedef struct {
	IOHIDDeviceRef dev;
	CFRunLoopRef rl;
	int stop;
	uint8_t buf[]; // IOKit puts input reports here
} hid;

int hid_list(uint64_t *ids, int max) {
	io_iterator_t it;
	if (IOServiceGetMatchingServices(MACH_PORT_NULL, IOServiceMatching(kIOHIDDeviceKey), &it) != KERN_SUCCESS)
		return 0;
	int n = 0;
	io_service_t s;
	while ((s = IOIteratorNext(it))) {
		uint64_t id;
		if (n < max && IORegistryEntryGetRegistryEntryID(s, &id) == KERN_SUCCESS)
			ids[n++] = id;
		IOObjectRelease(s);
	}
	IOObjectRelease(it);
	return n;
}

static io_service_t service(uint64_t id) {
	return IOServiceGetMatchingService(MACH_PORT_NULL, IORegistryEntryIDMatching(id));
}

int hid_descriptor(uint64_t id, uint8_t *buf, int size) {
	io_service_t s = service(id);
	if (!s)
		return -1;
	CFTypeRef p = IORegistryEntryCreateCFProperty(s, CFSTR(kIOHIDReportDescriptorKey), kCFAllocatorDefault, 0);
	IOObjectRelease(s);
	int n = -1;
	if (p && CFGetTypeID(p) == CFDataGetTypeID() && CFDataGetLength(p) <= size) {
		n = (int)CFDataGetLength(p);
		CFDataGetBytes(p, CFRangeMake(0, n), buf);
	}
	if (p)
		CFRelease(p);
	return n;
}

static void input_report(void *ctx, IOReturn result, void *sender, IOHIDReportType type, uint32_t id,
	uint8_t *report, CFIndex len) {
	if (result == kIOReturnSuccess)
		goInputReport((uintptr_t)ctx, report, (int)len);
}

static void removed(void *ctx, IOReturn result, void *sender) {
	goRemoved((uintptr_t)ctx);
}

void *hid_open(uint64_t id, int size, uintptr_t ctx, int *err) {
	io_service_t s = service(id);
	if (!s) {
		*err = kIOReturnNotFound;
		return NULL;
	}
	IOHIDDeviceRef dev = IOHIDDeviceCreate(kCFAllocatorDefault, s);
	IOObjectRelease(s);
	if (!dev) {
		*err = kIOReturnNoDevice;
		return NULL;
	}
	// shared, not seized: the configuration website may have it open too
	IOReturn r = IOHIDDeviceOpen(dev, kIOHIDOptionsTypeNone);
	if (r != kIOReturnSuccess) {
		CFRelease(dev);
		*err = r;
		return NULL;
	}
	hid *h = calloc(1, sizeof(hid) + size);
	h->dev = dev;
	IOHIDDeviceRegisterInputReportCallback(dev, h->buf, size, input_report, (void *)ctx);
	IOHIDDeviceRegisterRemovalCallback(dev, removed, (void *)ctx);
	return h;
}

// hid_run delivers the device's callbacks on this thread until hid_stop.
void hid_run(void *p) {
	hid *h = p;
	CFRunLoopRef rl = CFRunLoopGetCurrent();
	IOHIDDeviceScheduleWithRunLoop(h->dev, rl, kCFRunLoopDefaultMode);
	__atomic_store_n(&h->rl, rl, __ATOMIC_SEQ_CST);
	// a stop that comes before the loop runs is seen within a second
	while (!__atomic_load_n(&h->stop, __ATOMIC_SEQ_CST))
		CFRunLoopRunInMode(kCFRunLoopDefaultMode, 1, false);
	IOHIDDeviceUnscheduleFromRunLoop(h->dev, rl, kCFRunLoopDefaultMode);
}

void hid_stop(void *p) {
	hid *h = p;
	__atomic_store_n(&h->stop, 1, __ATOMIC_SEQ_CST);
	CFRunLoopRef rl = __atomic_load_n(&h->rl, __ATOMIC_SEQ_CST);
	if (rl)
		CFRunLoopStop(rl);
}

int hid_write(void *p, uint8_t *b, int n) {
	hid *h = p;
	return IOHIDDeviceSetReport(h->dev, kIOHIDReportTypeOutput, 0, b, n);
}

// hid_close closes the device once hid_run has returned.
void hid_close(void *p) {
	hid *h = p;
	IOHIDDeviceClose(h->dev, kIOHIDOptionsTypeNone);
	CFRelease(h->dev);
	free(h);
}

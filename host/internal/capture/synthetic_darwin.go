//go:build darwin && capture_integration

// Synthetic event posting for the integration test.
//
// This lives in a non-test file because cgo is not supported in _test.go
// files, and behind the capture_integration build tag so none of it is
// compiled into a shipping binary. Nothing here is used at runtime.
package capture

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework CoreGraphics

#include <ApplicationServices/ApplicationServices.h>

// Posting at kCGHIDEventTap puts these below the session tap, so the capture
// layer sees them exactly as it sees real input.

static void ehbTestPostKey(int keyCode, int down) {
  CGEventRef event = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)keyCode, down ? true : false);
  if (!event) return;
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

static void ehbTestPostMouseMove(int dx, int dy) {
  CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved,
                                             CGPointMake(300, 300), kCGMouseButtonLeft);
  if (!event) return;
  CGEventSetIntegerValueField(event, kCGMouseEventDeltaX, dx);
  CGEventSetIntegerValueField(event, kCGMouseEventDeltaY, dy);
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

// Absolute variant, for driving the cursor onto a screen edge where the
// capture layer reads the location rather than the delta.
static void ehbTestPostMouseMoveTo(double x, double y, int dx) {
  CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved,
                                             CGPointMake(x, y), kCGMouseButtonLeft);
  if (!event) return;
  CGEventSetIntegerValueField(event, kCGMouseEventDeltaX, dx);
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

static void ehbTestPostButton(int eventType, int buttonNumber) {
  CGEventRef event = CGEventCreateMouseEvent(NULL, (CGEventType)eventType,
                                             CGPointMake(300, 300), (CGMouseButton)buttonNumber);
  if (!event) return;
  CGEventSetIntegerValueField(event, kCGMouseEventButtonNumber, buttonNumber);
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

// Line units make IsContinuous == 0, i.e. the physical-wheel path.
static void ehbTestPostScrollLines(int vertical, int horizontal) {
  CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 2,
                                                   vertical, horizontal);
  if (!event) return;
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

static void ehbTestPostFlagsChanged(int keyCode, unsigned long long flags) {
  CGEventRef event = CGEventCreate(NULL);
  if (!event) return;
  CGEventSetType(event, kCGEventFlagsChanged);
  CGEventSetIntegerValueField(event, kCGKeyboardEventKeycode, keyCode);
  CGEventSetFlags(event, (CGEventFlags)flags);
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

static int ehbTestOtherMouseDown(void) { return kCGEventOtherMouseDown; }
static int ehbTestOtherMouseUp(void)   { return kCGEventOtherMouseUp; }
static int ehbTestLeftMouseDown(void)  { return kCGEventLeftMouseDown; }
static int ehbTestLeftMouseUp(void)    { return kCGEventLeftMouseUp; }
*/
import "C"

func syntheticKey(keyCode int, down bool) {
	pressed := 0
	if down {
		pressed = 1
	}
	C.ehbTestPostKey(C.int(keyCode), C.int(pressed))
}

func syntheticMouseMove(dx, dy int) {
	C.ehbTestPostMouseMove(C.int(dx), C.int(dy))
}

func syntheticMouseMoveTo(x, y float64, dx int) {
	C.ehbTestPostMouseMoveTo(C.double(x), C.double(y), C.int(dx))
}

func syntheticLeftButton(down bool) {
	eventType := C.ehbTestLeftMouseUp()
	if down {
		eventType = C.ehbTestLeftMouseDown()
	}
	C.ehbTestPostButton(eventType, 0)
}

func syntheticOtherButton(buttonNumber int, down bool) {
	eventType := C.ehbTestOtherMouseUp()
	if down {
		eventType = C.ehbTestOtherMouseDown()
	}
	C.ehbTestPostButton(eventType, C.int(buttonNumber))
}

func syntheticScrollLines(vertical, horizontal int) {
	C.ehbTestPostScrollLines(C.int(vertical), C.int(horizontal))
}

func syntheticFlagsChanged(keyCode int, flags uint64) {
	C.ehbTestPostFlagsChanged(C.int(keyCode), C.ulonglong(flags))
}

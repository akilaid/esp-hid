#include "capture_darwin.h"

#include <Carbon/Carbon.h> // IsSecureEventInputEnabled

// Implemented in Go (capture_darwin.go).
extern CGEventRef goCaptureTapCallback(CGEventTapProxy proxy, CGEventType type,
                                       CGEventRef event, void *userInfo);

#define EHB_MAX_DISPLAYS 32

CFMachPortRef ehbTapCreate(uintptr_t ctx) {
  // OtherMouseDragged is not optional: without it, motion is lost for as long
  // as a middle/back/forward button is held.
  CGEventMask mask = CGEventMaskBit(kCGEventMouseMoved) |
                     CGEventMaskBit(kCGEventLeftMouseDown) |
                     CGEventMaskBit(kCGEventLeftMouseUp) |
                     CGEventMaskBit(kCGEventRightMouseDown) |
                     CGEventMaskBit(kCGEventRightMouseUp) |
                     CGEventMaskBit(kCGEventOtherMouseDown) |
                     CGEventMaskBit(kCGEventOtherMouseUp) |
                     CGEventMaskBit(kCGEventScrollWheel) |
                     CGEventMaskBit(kCGEventKeyDown) |
                     CGEventMaskBit(kCGEventKeyUp) |
                     CGEventMaskBit(kCGEventFlagsChanged) |
                     CGEventMaskBit(kCGEventLeftMouseDragged) |
                     CGEventMaskBit(kCGEventRightMouseDragged) |
                     CGEventMaskBit(kCGEventOtherMouseDragged);

  // kCGEventTapOptionDefault makes this an active (filtering) tap, which is
  // what allows input to be swallowed — and what requires Accessibility
  // permission. A listen-only tap could not implement remote mode.
  return CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap,
                          kCGEventTapOptionDefault, mask, goCaptureTapCallback,
                          (void *)ctx);
}

void ehbTapEnable(CFMachPortRef tap, int enable) {
  if (tap) {
    CGEventTapEnable(tap, enable ? true : false);
  }
}

int ehbTapIsEnabled(CFMachPortRef tap) {
  return (tap && CGEventTapIsEnabled(tap)) ? 1 : 0;
}

CFRunLoopSourceRef ehbRunLoopSourceCreate(CFMachPortRef tap) {
  return CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0);
}

void ehbRunLoopAddSource(CFRunLoopSourceRef source) {
  CFRunLoopAddSource(CFRunLoopGetCurrent(), source, kCFRunLoopCommonModes);
}

void ehbRunLoopRemoveSource(CFRunLoopSourceRef source) {
  CFRunLoopRemoveSource(CFRunLoopGetCurrent(), source, kCFRunLoopCommonModes);
}

int ehbMachPortIsNull(CFMachPortRef port) { return port == NULL ? 1 : 0; }

int ehbRunLoopSourceIsNull(CFRunLoopSourceRef source) {
  return source == NULL ? 1 : 0;
}

void ehbReleaseMachPort(CFMachPortRef port) {
  if (port) {
    CFRelease(port);
  }
}

void ehbReleaseRunLoopSource(CFRunLoopSourceRef source) {
  if (source) {
    CFRelease(source);
  }
}

CFRunLoopRef ehbRunLoopCurrent(void) { return CFRunLoopGetCurrent(); }

void ehbRunLoopRun(void) { CFRunLoopRun(); }

void ehbRunLoopStop(CFRunLoopRef runLoop) {
  if (runLoop) {
    CFRunLoopStop(runLoop);
  }
}

static void ehbWatchdogFire(CFRunLoopTimerRef timer, void *info) {
  (void)timer;
  CFMachPortRef tap = (CFMachPortRef)info;
  if (tap && !CGEventTapIsEnabled(tap)) {
    CGEventTapEnable(tap, true);
  }
}

CFRunLoopTimerRef ehbWatchdogCreate(CFMachPortRef tap) {
  CFRunLoopTimerContext context = {0, (void *)tap, NULL, NULL, NULL};
  CFRunLoopTimerRef timer =
      CFRunLoopTimerCreate(kCFAllocatorDefault, CFAbsoluteTimeGetCurrent() + 1.0,
                           1.0, 0, 0, ehbWatchdogFire, &context);
  if (timer) {
    CFRunLoopAddTimer(CFRunLoopGetCurrent(), timer, kCFRunLoopCommonModes);
  }
  return timer;
}

void ehbWatchdogInvalidate(CFRunLoopTimerRef timer) {
  if (timer) {
    CFRunLoopTimerInvalidate(timer);
    CFRelease(timer);
  }
}

int64_t ehbEventKeyCode(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
}

uint64_t ehbEventFlags(CGEventRef event) {
  return (uint64_t)CGEventGetFlags(event);
}

int64_t ehbEventDeltaX(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGMouseEventDeltaX);
}

int64_t ehbEventDeltaY(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGMouseEventDeltaY);
}

int64_t ehbEventButtonNumber(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGMouseEventButtonNumber);
}

int64_t ehbEventIsAutorepeat(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat);
}

int64_t ehbEventScrollIsContinuous(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGScrollWheelEventIsContinuous);
}

int64_t ehbEventScrollLineV(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGScrollWheelEventDeltaAxis1);
}

int64_t ehbEventScrollLineH(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGScrollWheelEventDeltaAxis2);
}

int64_t ehbEventScrollPointV(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGScrollWheelEventPointDeltaAxis1);
}

int64_t ehbEventScrollPointH(CGEventRef event) {
  return CGEventGetIntegerValueField(event, kCGScrollWheelEventPointDeltaAxis2);
}

double ehbEventLocationX(CGEventRef event) {
  return CGEventGetLocation(event).x;
}

double ehbEventLocationY(CGEventRef event) {
  return CGEventGetLocation(event).y;
}

CGEventRef ehbNullEvent(void) { return NULL; }

void ehbWarpCursor(double x, double y) {
  CGWarpMouseCursorPosition(CGPointMake(x, y));
}

void ehbSetMouseAssociation(int associated) {
  CGAssociateMouseAndMouseCursorPosition(associated ? true : false);
}

// CGDisplayHideCursor is honoured only while this process is the frontmost
// application, which remote mode generally is not — minimize the window and
// the pointer reappears. There is no public way around that. This connection
// property is the way around it, and has been since roughly 10.5; Synergy and
// its descendants have relied on it for as long.
//
// Private, so treated as optional: measured on macOS 26, and if a future
// release drops it the call fails, the cursor stays visible, and nothing else
// changes. Declared here because it appears in no public header.
typedef int CGSConnectionID;
extern CGSConnectionID CGSMainConnectionID(void);
extern CGError CGSSetConnectionProperty(CGSConnectionID cid,
                                        CGSConnectionID target, CFStringRef key,
                                        CFTypeRef value);

int ehbEnableBackgroundCursor(void) {
  CGSConnectionID cid = CGSMainConnectionID();
  return CGSSetConnectionProperty(cid, cid, CFSTR("SetsCursorInBackground"),
                                  kCFBooleanTrue) == kCGErrorSuccess;
}

static int ehbActiveDisplays(CGDirectDisplayID *ids, uint32_t max) {
  uint32_t count = 0;
  if (CGGetActiveDisplayList(max, ids, &count) != kCGErrorSuccess) {
    return 0;
  }
  return (int)count;
}

// One call, not one per display. CGDisplayHideCursor ignores its display
// argument entirely (CGDirectDisplay.h: "The `display' parameter is ignored")
// and increments a single per-connection hide count, so looping over the
// active display list only inflated that count. It also made the count depend
// on how many displays were attached: plug or unplug a monitor while remote
// mode was engaged and the later shows no longer balanced the hides, leaving
// the cursor either permanently invisible or visible too early.
//
// The flag makes both calls idempotent, so a re-assert costs nothing.
static int gCursorHidden = 0;

void ehbHideCursor(void) {
  if (!gCursorHidden) {
    CGDisplayHideCursor(kCGDirectMainDisplay);
    gCursorHidden = 1;
  }
}

void ehbShowCursor(void) {
  if (gCursorHidden) {
    CGDisplayShowCursor(kCGDirectMainDisplay);
    gCursorHidden = 0;
  }
}

void ehbCursorPosition(double *x, double *y) {
  CGEventRef probe = CGEventCreate(NULL);
  if (!probe) {
    *x = 0;
    *y = 0;
    return;
  }
  CGPoint location = CGEventGetLocation(probe);
  CFRelease(probe);
  *x = location.x;
  *y = location.y;
}

uint64_t ehbCurrentFlags(void) {
  CGEventRef probe = CGEventCreate(NULL);
  if (!probe) {
    return 0;
  }
  uint64_t flags = (uint64_t)CGEventGetFlags(probe);
  CFRelease(probe);
  return flags;
}

int ehbDisplayBounds(double *out, int maxCount) {
  CGDirectDisplayID ids[EHB_MAX_DISPLAYS];
  int count = ehbActiveDisplays(ids, EHB_MAX_DISPLAYS);
  if (count > maxCount) {
    count = maxCount;
  }
  for (int i = 0; i < count; i++) {
    CGRect bounds = CGDisplayBounds(ids[i]);
    out[i * 4 + 0] = bounds.origin.x;
    out[i * 4 + 1] = bounds.origin.y;
    out[i * 4 + 2] = bounds.size.width;
    out[i * 4 + 3] = bounds.size.height;
  }
  return count;
}

int ehbHasAccessibility(int prompt) {
  if (!prompt) {
    return AXIsProcessTrusted() ? 1 : 0;
  }
  CFStringRef keys[] = {kAXTrustedCheckOptionPrompt};
  CFBooleanRef values[] = {kCFBooleanTrue};
  CFDictionaryRef options = CFDictionaryCreate(
      kCFAllocatorDefault, (const void **)keys, (const void **)values, 1,
      &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
  Boolean trusted = AXIsProcessTrustedWithOptions(options);
  if (options) {
    CFRelease(options);
  }
  return trusted ? 1 : 0;
}

int ehbHasInputMonitoring(int request) {
  if (request) {
    return CGRequestListenEventAccess() ? 1 : 0;
  }
  return CGPreflightListenEventAccess() ? 1 : 0;
}

int ehbSecureInputEnabled(void) { return IsSecureEventInputEnabled() ? 1 : 0; }

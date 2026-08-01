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

static int ehbActiveDisplays(CGDirectDisplayID *ids, uint32_t max) {
  uint32_t count = 0;
  if (CGGetActiveDisplayList(max, ids, &count) != kCGErrorSuccess) {
    return 0;
  }
  return (int)count;
}

// Hiding on every display rather than just the main one matters: the cursor
// is usually parked over some other app's window when remote mode engages.
void ehbHideCursorAllDisplays(void) {
  CGDirectDisplayID ids[EHB_MAX_DISPLAYS];
  int count = ehbActiveDisplays(ids, EHB_MAX_DISPLAYS);
  if (count == 0) {
    CGDisplayHideCursor(kCGDirectMainDisplay);
    return;
  }
  for (int i = 0; i < count; i++) {
    CGDisplayHideCursor(ids[i]);
  }
}

void ehbShowCursorAllDisplays(void) {
  CGDirectDisplayID ids[EHB_MAX_DISPLAYS];
  int count = ehbActiveDisplays(ids, EHB_MAX_DISPLAYS);
  if (count == 0) {
    CGDisplayShowCursor(kCGDirectMainDisplay);
    return;
  }
  for (int i = 0; i < count; i++) {
    CGDisplayShowCursor(ids[i]);
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

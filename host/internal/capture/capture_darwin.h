// C shims for the macOS capture layer.
//
// Kept in a real .c/.h pair rather than a cgo preamble comment: cgo compiles
// sibling C files in the package automatically, the _darwin suffix gives them
// the same GOOS constraint as the Go file, and the code stays readable to
// clang-format and to editors.
//
// Two kinds of shim live here. Some wrap calls cgo cannot express directly
// (passing a Go uintptr as a void* refcon, taking the address of a C
// function). The rest exist because CoreFoundation opaque types cannot be
// compared against nil from Go.

#ifndef ESP_HID_CAPTURE_DARWIN_H
#define ESP_HID_CAPTURE_DARWIN_H

#include <ApplicationServices/ApplicationServices.h>
#include <stdint.h>

// --- Tap lifecycle -------------------------------------------------------

// Creates the session-wide, actively filtering event tap. ctx is handed back
// to the Go callback as the refcon; it carries a runtime/cgo.Handle.
CFMachPortRef ehbTapCreate(uintptr_t ctx);
void ehbTapEnable(CFMachPortRef tap, int enable);
int ehbTapIsEnabled(CFMachPortRef tap);

CFRunLoopSourceRef ehbRunLoopSourceCreate(CFMachPortRef tap);
void ehbRunLoopAddSource(CFRunLoopSourceRef source);
void ehbRunLoopRemoveSource(CFRunLoopSourceRef source);

// Null checks: CFMachPortRef / CFRunLoopSourceRef cannot be compared to nil
// in Go.
int ehbMachPortIsNull(CFMachPortRef port);
int ehbRunLoopSourceIsNull(CFRunLoopSourceRef source);
void ehbReleaseMachPort(CFMachPortRef port);
void ehbReleaseRunLoopSource(CFRunLoopSourceRef source);

// --- Run loop ------------------------------------------------------------

CFRunLoopRef ehbRunLoopCurrent(void);
void ehbRunLoopRun(void);
// Safe to call from another thread; CFRunLoopStop is thread-safe.
void ehbRunLoopStop(CFRunLoopRef runLoop);

// A 1 Hz timer that re-enables the tap if the window server disabled it.
// Belt and braces alongside the in-callback recovery: a tap can be disabled
// without a disable event ever being delivered.
CFRunLoopTimerRef ehbWatchdogCreate(CFMachPortRef tap);
void ehbWatchdogInvalidate(CFRunLoopTimerRef timer);

// --- Event accessors -----------------------------------------------------

int64_t ehbEventKeyCode(CGEventRef event);
uint64_t ehbEventFlags(CGEventRef event);
int64_t ehbEventDeltaX(CGEventRef event);
int64_t ehbEventDeltaY(CGEventRef event);
int64_t ehbEventButtonNumber(CGEventRef event);
int64_t ehbEventIsAutorepeat(CGEventRef event);
int64_t ehbEventScrollIsContinuous(CGEventRef event);
int64_t ehbEventScrollLineV(CGEventRef event);
int64_t ehbEventScrollLineH(CGEventRef event);
int64_t ehbEventScrollPointV(CGEventRef event);
int64_t ehbEventScrollPointH(CGEventRef event);
double ehbEventLocationX(CGEventRef event);
double ehbEventLocationY(CGEventRef event);
// Returning NULL from the tap callback swallows the event.
CGEventRef ehbNullEvent(void);

// --- Cursor and displays -------------------------------------------------

void ehbWarpCursor(double x, double y);
// Decoupling the cursor from the hardware is what makes relative capture
// work without warping on every event.
void ehbSetMouseAssociation(int associated);
// Idempotent, and display-count independent: CGDisplayHideCursor ignores its
// display argument and keeps one hide count per window server connection.
void ehbHideCursor(void);
void ehbShowCursor(void);
void ehbCursorPosition(double *x, double *y);
uint64_t ehbCurrentFlags(void);
// Fills out with {x, y, w, h} per display; returns the number written.
int ehbDisplayBounds(double *out, int maxCount);

// Lifts the frontmost-application restriction on ehbHideCursor. Call once at
// startup; returns non-zero on success. See the note by the implementation.
int ehbEnableBackgroundCursor(void);

// --- Permissions ---------------------------------------------------------

// prompt != 0 raises the system Accessibility dialog.
int ehbHasAccessibility(int prompt);
// request != 0 raises the system Input Monitoring dialog.
int ehbHasInputMonitoring(int request);
// Secure Event Input blocks keyboard events from reaching every tap.
int ehbSecureInputEnabled(void);

#endif // ESP_HID_CAPTURE_DARWIN_H

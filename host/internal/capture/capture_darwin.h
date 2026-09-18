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
// A warp starts an interval (0.25 s by default) during which hardware motion
// does not move the pointer. That is the hesitation felt on returning to the
// host: the exit warps the pointer to the edge and the next quarter second
// of mouse movement goes nowhere. Re-associating straight after the warp is
// the documented way to cut it short, but that call is honoured only for
// the frontmost application, which the bridge normally is not. This sets
// the interval for the whole window-server connection instead. The app
// never posts events, so zero costs nothing. Call once at startup.
void ehbSetLocalEventsSuppression(double seconds);
// Idempotent, and display-count independent: CGDisplayHideCursor ignores its
// display argument and keeps one hide count per window server connection.
//
// Returns non-zero when the window server agrees the cursor is now hidden.
// It refuses while the Dock is tracking the pointer — from the moment a
// mouse *event* lands anywhere in the Dock's strip until one lands outside
// it; warps do not count either way. See ehbPostRelocation.
//
// A hide that did not take is undone with a show before returning, so hides
// and shows always pair off whatever the window server made of it. Without
// that, a hide it counted but overrode was hidden again on every retry, and
// the single show on exit could not bring the pointer back.
int ehbHideCursor(void);
// Shows the cursor if this session hid it, or unconditionally when force is
// set — for a caller that has confirmed on a later event that the pointer
// is still missing and wants to unwind a hide count it did not know about.
// Returns non-zero when the window server reports the cursor visible. A
// show takes ~150us to be reflected (measured; a hide is immediate), so the
// value read straight after one is stale: confirm on a later event.
int ehbShowCursor(int force);
// Posts a real mouse-moved event to (x, y), tagged so the tap can recognise
// it. This is how the pointer is taken away from the Dock: only an event
// makes the Dock stop tracking, and a tracked pointer can be neither hidden
// nor warped. The tap must pass the event through untouched, or the Dock
// never sees it.
void ehbPostRelocation(double x, double y);
int ehbEventIsRelocation(CGEventRef event);
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

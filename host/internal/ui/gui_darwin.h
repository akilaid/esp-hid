// C interface for the macOS AppKit GUI.
//
// The boundary is deliberately narrow and value-typed: no AppKit object ever
// crosses into Go, and no Go pointer is retained by Objective-C. Everything
// passes as scalars or NUL-terminated strings, and the form is read back as a
// struct returned by value, which sidesteps ownership questions entirely.
//
// Every function here must be called on the main thread. Go code that is not
// already on it goes through ehbGuiPerformOnMain.

#ifndef ESP_HID_GUI_DARWIN_H
#define ESP_HID_GUI_DARWIN_H

#include <stdint.h>

// Snapshot of the settings form. Fixed-size buffers so it can be returned by
// value with no allocation on either side.
typedef struct {
  char hotkey[64];
  char resolution[32];
  int rateHz;
  int captureKeyboard;
  int autoSwitch;
  int anyDisplay;
  int edgePush;
  int edgePushForce;
} EhbForm;

// One monitor for the arrangement picture: its place in the desktop's
// global coordinate space (points, y down — the space CGDisplayBounds and
// the capture layer use) and its physical size, 0 when unknown.
typedef struct {
  double x, y, w, h;
  double widthMM, heightMM;
  int primary;
  char name[64];
} EhbDisplay;

typedef struct {
  double x, y, w, h;
} EhbRect;

// --- Lifecycle -----------------------------------------------------------

// Builds the application, menu bar, window and status item. Must run on the
// main thread before anything else here.
void ehbGuiInit(void);
// Enters [NSApp run]. Does not return until the app terminates.
void ehbGuiRun(void);
void ehbGuiTerminate(void);

// --- Populating the form -------------------------------------------------

void ehbGuiAddResolution(const char *value);
void ehbGuiAddOrientation(const char *value);
void ehbGuiSetForm(const char *hotkey, int rateHz, int captureKeyboard,
                   int autoSwitch, int anyDisplay, int edgePush,
                   int edgePushForce, const char *resolution);
EhbForm ehbGuiReadForm(void);

// --- Display arrangement -------------------------------------------------
//
// The picture that replaced the "This Mac sits" popup. Go owns the model
// (ui/arrange.go): it asks for the displays and the view's size, works out
// every rectangle in the view's own coordinates (y down), and pushes them
// between Begin and End. The view draws what it was given and reports mouse
// events through goGuiArrangeMouse; it decides nothing.

// Fills out with the active displays; returns the number written.
int ehbGuiDisplays(EhbDisplay *out, int max);
void ehbGuiArrangeSize(double *width, double *height);
void ehbGuiArrangeBegin(int dragging);
void ehbGuiArrangeAddDisplay(EhbRect rect, const char *name, int primary);
void ehbGuiArrangeSetDevice(EhbRect rect, const char *label);
void ehbGuiArrangeEnd(void);

// The device picker and orientation toggle are helpers that write into the
// resolution field; only the resolution is read back. Go owns the search and
// the match list, and pushes the rows here as plain strings: label is what
// the list shows, name is what the field reads once a row is picked. Show
// lays the list out under the field, or hides it when there are no rows.
void ehbGuiClearDeviceMatches(void);
void ehbGuiAddDeviceMatch(const char *label, const char *name);
void ehbGuiShowDeviceMatches(void);
void ehbGuiSelectDeviceMatch(int index);
void ehbGuiSetResolution(const char *value);
void ehbGuiSetOrientation(int index);
void ehbGuiSetResolutionHint(const char *text);

// --- Updating the display ------------------------------------------------

void ehbGuiSetStatus(const char *bridge, const char *device,
                     const char *firmware, const char *bluetooth);
void ehbGuiSetRunning(int running);
// Swaps the menu-bar image so remote mode is visible at a glance — the one
// piece of feedback the legacy macOS app never surfaced.
void ehbGuiSetRemoteActive(int active);
// The strip above the status rows: a missing permission, Secure Input, or an
// available update. buttons selects which row of buttons accompanies it —
// EHB_BANNER_PERMISSION (Grant / Open System Settings) or EHB_BANNER_UPDATE
// (Install and relaunch) — and isError picks the red text. When hidden the
// strip collapses and the window shrinks with it.
enum { EHB_BANNER_NONE = 0, EHB_BANNER_PERMISSION = 1, EHB_BANNER_UPDATE = 2 };
void ehbGuiSetBanner(const char *message, int visible, int buttons, int isError);
// The check mark on the "Check for Updates Automatically" menu item.
void ehbGuiSetAutoUpdateChecked(int checked);
// The running version, shown in the window's footer beside Check for Updates.
void ehbGuiSetVersion(const char *text);
void ehbGuiShowAlert(const char *title, const char *message, int isError);
// The update prompt: message plus the release notes in a scrolling box, with
// "Install and Relaunch" and "Later". Returns 1 to install, 0 otherwise.
int ehbGuiAskUpdate(const char *title, const char *message, const char *notes);

// anchor is a System Settings pane anchor, e.g. "Privacy_Accessibility".
void ehbGuiOpenPrivacySettings(const char *anchor);

// --- Main-thread trampoline ----------------------------------------------

// Runs the Go closure identified by token on the main thread. The analogue
// of walk's Synchronize on the Windows side.
void ehbGuiPerformOnMain(uintptr_t token);

#endif // ESP_HID_GUI_DARWIN_H

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
  int hostSideIndex;
} EhbForm;

// --- Lifecycle -----------------------------------------------------------

// Builds the application, menu bar, window and status item. Must run on the
// main thread before anything else here.
void ehbGuiInit(void);
// Enters [NSApp run]. Does not return until the app terminates.
void ehbGuiRun(void);
void ehbGuiTerminate(void);

// --- Populating the form -------------------------------------------------

void ehbGuiAddResolution(const char *value);
void ehbGuiAddHostSide(const char *value);
void ehbGuiSetForm(const char *hotkey, int rateHz, int captureKeyboard,
                   int autoSwitch, const char *resolution, int hostSideIndex);
EhbForm ehbGuiReadForm(void);

// --- Updating the display ------------------------------------------------

void ehbGuiSetStatus(const char *bridge, const char *device,
                     const char *firmware, const char *bluetooth);
void ehbGuiSetRunning(int running);
// Swaps the menu-bar image so remote mode is visible at a glance — the one
// piece of feedback the legacy macOS app never surfaced.
void ehbGuiSetRemoteActive(int active);
// A missing-permission (or secure-input) banner above the status rows.
void ehbGuiSetBanner(const char *message, int visible, int showGrantButtons);
void ehbGuiShowAlert(const char *title, const char *message, int isError);

// anchor is a System Settings pane anchor, e.g. "Privacy_Accessibility".
void ehbGuiOpenPrivacySettings(const char *anchor);

// --- Main-thread trampoline ----------------------------------------------

// Runs the Go closure identified by token on the main thread. The analogue
// of walk's Synchronize on the Windows side.
void ehbGuiPerformOnMain(uintptr_t token);

#endif // ESP_HID_GUI_DARWIN_H

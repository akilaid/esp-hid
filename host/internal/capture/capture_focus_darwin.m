// Holding the foreground for the duration of remote mode.
//
// The two calls that make remote mode work — CGAssociateMouseAndMouseCursor-
// Position(false) and CGDisplayHideCursor — are honoured only while this
// process is the frontmost application. CGRemoteOperation.h states it plainly:
// "Connect or disconnect the mouse and cursor while an application is in the
// foreground." The event tap is a session tap and keeps capturing regardless,
// so when the bridge is not frontmost the symptom is the confusing one: the
// remote device is driven correctly *and* the Mac's own pointer moves and
// stays visible at the same time.
//
// Asking to be activated is not enough on its own. macOS 14 tightened what a
// background app may do, and an activation request from an app whose windows
// are all minimized has nothing to bring forward — it is quietly ignored,
// which is exactly the case users hit by minimizing the window and pressing
// the hotkey. So the grab orders a window front: one point square, fully
// transparent, mouse-transparent, excluded from the Windows menu and from
// window cycling. Nothing is visible and nothing is un-minimized; the window
// exists only to give the window server something to activate.
//
// This is the only file in the capture package that touches AppKit. The
// window is a mechanism for holding focus, not an interface — AppKit proper
// still lives in internal/ui.

#import <AppKit/AppKit.h>
#include <stdatomic.h>

#include "capture_darwin.h"

// Bumped by every grab and every release. A grab does its work asynchronously
// and polls for activation to land, so a fast toggle off can overtake it; the
// queued block compares against this and abandons a superseded grab rather
// than re-dissociating the mouse after remote mode has already ended, which
// would leave the pointer dead.
static _Atomic int gFocusGeneration = 0;

// Whoever was frontmost when remote mode began, so exiting hands focus back.
// nil when that was us, making release a no-op in the common case. Touched
// only from the serial queue below.
static NSRunningApplication *gPrevApp = nil;

// Serial, so grab and release stay ordered and gPrevApp needs no lock. Not the
// main queue: headless mode (-gui=false) never pumps it, and the polling below
// would stall the UI if it did run there.
static dispatch_queue_t focusQueue(void) {
  static dispatch_queue_t queue;
  static dispatch_once_t once;
  dispatch_once(&once, ^{
    queue = dispatch_queue_create("gg.sen-net.esp-hid.focus",
                                  DISPATCH_QUEUE_SERIAL);
  });
  return queue;
}

// A borderless NSWindow refuses to become key, which would defeat the whole
// purpose, so canBecomeKeyWindow has to be overridden.
@interface EhbFocusWindow : NSWindow
@end

@implementation EhbFocusWindow
- (BOOL)canBecomeKeyWindow {
  return YES;
}
- (BOOL)canBecomeMainWindow {
  return NO;
}
@end

static EhbFocusWindow *gFocusWindow = nil;

// Main thread only.
static EhbFocusWindow *ensureFocusWindow(void) {
  if (gFocusWindow != nil) {
    return gFocusWindow;
  }
  gFocusWindow =
      [[EhbFocusWindow alloc] initWithContentRect:NSMakeRect(0, 0, 1, 1)
                                        styleMask:NSWindowStyleMaskBorderless
                                          backing:NSBackingStoreBuffered
                                            defer:NO];
  // Clear rather than alpha 0: an entirely transparent window still takes part
  // in activation, whereas a zero-alpha one is not reliably treated as on
  // screen at all.
  [gFocusWindow setOpaque:NO];
  [gFocusWindow setBackgroundColor:[NSColor clearColor]];
  [gFocusWindow setHasShadow:NO];
  // Clicks must keep reaching the tap, not this window.
  [gFocusWindow setIgnoresMouseEvents:YES];
  [gFocusWindow setLevel:NSScreenSaverWindowLevel];
  // Follow the user to whichever Space they are on, and stay out of Mission
  // Control, the Windows menu and Cmd-` cycling.
  [gFocusWindow setCollectionBehavior:NSWindowCollectionBehaviorCanJoinAllSpaces |
                                      NSWindowCollectionBehaviorStationary |
                                      NSWindowCollectionBehaviorIgnoresCycle |
                                      NSWindowCollectionBehaviorFullScreenAuxiliary];
  [gFocusWindow setExcludedFromWindowsMenu:YES];
  [gFocusWindow setReleasedWhenClosed:NO];
  return gFocusWindow;
}

// Main thread only. NSApp is nil when running headless, where there is no
// application object to activate and no main queue being pumped anyway.
static void showFocusWindow(void) {
  if (NSApp == nil) {
    return;
  }
  EhbFocusWindow *window = ensureFocusWindow();
  // Park it under the pointer so it lands on the display the user is actually
  // looking at. One point square, so nothing is drawn over.
  NSPoint cursor = [NSEvent mouseLocation];
  [window setFrameOrigin:NSMakePoint(cursor.x, cursor.y)];
  [window makeKeyAndOrderFront:nil];
  if (@available(macOS 14.0, *)) {
    [NSApp activate];
  } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
    [NSApp activateIgnoringOtherApps:YES];
#pragma clang diagnostic pop
  }
}

// Main thread only.
static void hideFocusWindow(void) {
  if (gFocusWindow != nil) {
    [gFocusWindow orderOut:nil];
  }
}

static void activateApp(NSRunningApplication *app) {
  if (@available(macOS 14.0, *)) {
    [app activateWithOptions:0];
  } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
    [app activateWithOptions:NSApplicationActivateIgnoringOtherApps];
#pragma clang diagnostic pop
  }
}

void ehbFocusGrab(void) {
  int generation = atomic_fetch_add(&gFocusGeneration, 1) + 1;
  dispatch_async(focusQueue(), ^{
    if (atomic_load(&gFocusGeneration) != generation) {
      return;
    }
    NSRunningApplication *me = [NSRunningApplication currentApplication];
    NSRunningApplication *previous =
        [[NSWorkspace sharedWorkspace] frontmostApplication];
    [gPrevApp release];
    gPrevApp = (previous == nil || [previous isEqual:me]) ? nil
                                                          : [previous retain];

    // The window is what actually makes this work when every window is
    // minimized; activateWithOptions alone covers the headless case, where
    // there is no NSApp and the main queue is never drained.
    dispatch_async(dispatch_get_main_queue(), ^{
      if (atomic_load(&gFocusGeneration) != generation) {
        return;
      }
      showFocusWindow();
    });
    activateApp(me);

    // Activation is asynchronous — it returns long before the window server
    // agrees we are frontmost. Wait for it to land, then re-assert the
    // dissociation, which was a no-op if it ran while we were still in the
    // background. The cursor hide needs no re-assert: losing the foreground
    // stops the hide being *honoured*, it does not reset the hide count.
    for (int i = 0; i < 20 && ![me isActive]; i++) {
      if (atomic_load(&gFocusGeneration) != generation) {
        return;
      }
      usleep(25 * 1000);
    }
    if (atomic_load(&gFocusGeneration) != generation) {
      return;
    }
    if (![me isActive]) {
      NSLog(@"esp-hid: could not bring the app to the foreground; the local "
            @"cursor may stay visible while remote mode is active");
    }
    ehbSetMouseAssociation(0);
    ehbHideCursor();
  });
}

void ehbFocusRelease(void) {
  // Synchronous, and load-bearing: it happens on the caller's thread before
  // this function returns, which is what lets the caller re-associate the
  // mouse knowing no in-flight grab can undo it afterwards. Callers therefore
  // invoke this *before* CGAssociateMouseAndMouseCursorPosition(true), not
  // after. Handing focus back is the part that gets queued.
  atomic_fetch_add(&gFocusGeneration, 1);
  dispatch_async(focusQueue(), ^{
    dispatch_async(dispatch_get_main_queue(), ^{
      hideFocusWindow();
    });
    NSRunningApplication *previous = gPrevApp;
    gPrevApp = nil;
    if (previous == nil) {
      return;
    }
    activateApp(previous);
    [previous release];
  });
}

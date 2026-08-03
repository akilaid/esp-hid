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
// This is the only file in the capture package that touches AppKit, and it
// uses it for exactly two process-level calls. No UI, no windows, no NSApp
// ownership — AppKit proper still lives in internal/ui.

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
// nil when that was us, making release a no-op in the common case.
static NSRunningApplication *gPrevApp = nil;

// Serial, so grab and release stay ordered and gPrevApp needs no lock. Not the
// main queue: headless mode (-gui=false) never pumps it.
static dispatch_queue_t focusQueue(void) {
  static dispatch_queue_t queue;
  static dispatch_once_t once;
  dispatch_once(&once, ^{
    queue = dispatch_queue_create("gg.sen-net.esp-hid.focus",
                                  DISPATCH_QUEUE_SERIAL);
  });
  return queue;
}

static void activateSelf(NSRunningApplication *me) {
  // Ordering an NSApp activation alongside the NSRunningApplication one makes
  // it stick more reliably when the app has no visible window. NSApp is nil in
  // headless mode, where this whole branch is skipped.
  if (NSApp != nil) {
    dispatch_async(dispatch_get_main_queue(), ^{
      if (@available(macOS 14.0, *)) {
        [NSApp activate];
      } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        [NSApp activateIgnoringOtherApps:YES];
#pragma clang diagnostic pop
      }
    });
  }
  if (@available(macOS 14.0, *)) {
    [me activateWithOptions:0];
  } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
    [me activateWithOptions:NSApplicationActivateIgnoringOtherApps];
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

    activateSelf(me);

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
    NSRunningApplication *previous = gPrevApp;
    gPrevApp = nil;
    if (previous == nil) {
      return;
    }
    if (@available(macOS 14.0, *)) {
      [previous activateWithOptions:0];
    } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
      [previous activateWithOptions:NSApplicationActivateIgnoringOtherApps];
#pragma clang diagnostic pop
    }
    [previous release];
  });
}

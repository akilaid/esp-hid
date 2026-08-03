#import <Cocoa/Cocoa.h>

#include "gui_darwin.h"

// Implemented in Go (gui_darwin.go).
extern void goGuiStartClicked(void);
extern void goGuiStopClicked(void);
extern void goGuiClearBondsClicked(void);
extern void goGuiGrantClicked(void);
extern void goGuiOpenSettingsClicked(void);
extern void goGuiTick(void);
extern void goGuiWillTerminate(void);
extern void goGuiPerform(uintptr_t token);

// Fixed-size window: the layout is hand-placed, which is a fair trade for a
// settings form that never needs to resize and keeps this file free of
// constraint plumbing.
static const CGFloat kWindowWidth = 620;
static const CGFloat kWindowHeight = 560;
static const CGFloat kMargin = 20;
static const CGFloat kRowHeight = 22;

@interface EHBController : NSObject <NSApplicationDelegate, NSWindowDelegate>
@end

static EHBController *gController = nil;
static NSWindow *gWindow = nil;
static NSStatusItem *gStatusItem = nil;
static NSImage *gIconIdle = nil;
static NSImage *gIconActive = nil;

static NSTextField *gStatusBridge = nil;
static NSTextField *gStatusDevice = nil;
static NSTextField *gStatusFirmware = nil;
static NSTextField *gStatusBluetooth = nil;

static NSTextField *gBanner = nil;
static NSButton *gGrantButton = nil;
static NSButton *gSettingsButton = nil;

static NSButton *gStartButton = nil;
static NSButton *gStopButton = nil;
static NSButton *gBondsButton = nil;

static NSTextField *gHotkeyField = nil;
static NSTextField *gRateField = nil;
static NSButton *gKeyboardCheck = nil;
static NSComboBox *gResolutionCombo = nil;
static NSPopUpButton *gHostSidePopup = nil;

static NSTextField *makeLabel(NSView *parent, NSString *text, CGFloat x,
                              CGFloat y, CGFloat width, BOOL bold) {
  NSTextField *field = [[NSTextField alloc] initWithFrame:NSMakeRect(x, y, width, kRowHeight)];
  [field setStringValue:text];
  [field setBezeled:NO];
  [field setDrawsBackground:NO];
  [field setEditable:NO];
  [field setSelectable:NO];
  [field setFont:bold ? [NSFont boldSystemFontOfSize:13] : [NSFont systemFontOfSize:13]];
  [parent addSubview:field];
  return field;
}

// Status values are selectable so a user can copy a port name or version
// straight out of the window when reporting a problem.
static NSTextField *makeValue(NSView *parent, NSString *text, CGFloat x,
                              CGFloat y, CGFloat width) {
  NSTextField *field = makeLabel(parent, text, x, y, width, NO);
  [field setSelectable:YES];
  [field setTextColor:[NSColor secondaryLabelColor]];
  return field;
}

static NSButton *makeButton(NSView *parent, NSString *title, CGFloat x,
                            CGFloat y, CGFloat width, SEL action) {
  NSButton *button = [[NSButton alloc] initWithFrame:NSMakeRect(x, y, width, 26)];
  [button setTitle:title];
  [button setBezelStyle:NSBezelStyleRounded];
  [button setTarget:gController];
  [button setAction:action];
  [parent addSubview:button];
  return button;
}

static NSTextField *makeField(NSView *parent, CGFloat x, CGFloat y,
                              CGFloat width) {
  NSTextField *field = [[NSTextField alloc] initWithFrame:NSMakeRect(x, y, width, 24)];
  [field setBezeled:YES];
  [field setDrawsBackground:YES];
  [field setEditable:YES];
  [field setSelectable:YES];
  [parent addSubview:field];
  return field;
}

static NSBox *makeBox(NSView *parent, NSString *title, CGFloat y,
                      CGFloat height) {
  NSBox *box = [[NSBox alloc]
      initWithFrame:NSMakeRect(kMargin, y, kWindowWidth - 2 * kMargin, height)];
  [box setTitle:title];
  [box setBoxType:NSBoxPrimary];
  [parent addSubview:box];
  return box;
}

@implementation EHBController

- (void)startClicked:(id)sender {
  (void)sender;
  goGuiStartClicked();
}

- (void)stopClicked:(id)sender {
  (void)sender;
  goGuiStopClicked();
}

- (void)bondsClicked:(id)sender {
  (void)sender;
  goGuiClearBondsClicked();
}

- (void)grantClicked:(id)sender {
  (void)sender;
  goGuiGrantClicked();
}

- (void)settingsClicked:(id)sender {
  (void)sender;
  goGuiOpenSettingsClicked();
}

- (void)openWindow:(id)sender {
  (void)sender;
  [gWindow makeKeyAndOrderFront:nil];
  [NSApp activateIgnoringOtherApps:YES];
}

- (void)tick:(NSTimer *)timer {
  (void)timer;
  goGuiTick();
}

// Closing the window hides it, matching the Windows build where the X button
// minimises to the tray. Quitting is done from the menu bar.
- (BOOL)windowShouldClose:(NSWindow *)sender {
  [sender orderOut:nil];
  return NO;
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)app {
  (void)app;
  return NO;
}

// Safety-critical: remote mode leaves the pointer hidden and decoupled from
// the mouse, and only the capture layer's teardown restores it. Quitting
// without stopping the bridge first would strand the user with a frozen,
// invisible cursor.
- (void)applicationWillTerminate:(NSNotification *)note {
  (void)note;
  goGuiWillTerminate();
}

@end

static void buildMenuBar(void) {
  // With no nib there is no menu bar at all, and Cmd-Q does nothing, so it
  // has to be constructed by hand.
  NSMenu *menuBar = [[NSMenu alloc] init];
  NSMenuItem *appItem = [[NSMenuItem alloc] init];
  [menuBar addItem:appItem];
  [NSApp setMainMenu:menuBar];

  NSMenu *appMenu = [[NSMenu alloc] init];
  [appMenu addItemWithTitle:@"About ESP HID Bridge"
                     action:@selector(orderFrontStandardAboutPanel:)
              keyEquivalent:@""];
  [appMenu addItem:[NSMenuItem separatorItem]];
  [appMenu addItemWithTitle:@"Hide ESP HID Bridge"
                     action:@selector(hide:)
              keyEquivalent:@"h"];
  [appMenu addItem:[NSMenuItem separatorItem]];
  [appMenu addItemWithTitle:@"Quit ESP HID Bridge"
                     action:@selector(terminate:)
              keyEquivalent:@"q"];
  [appItem setSubmenu:appMenu];

  // A minimal Edit menu so the standard clipboard shortcuts work in the
  // hotkey and resolution fields.
  NSMenuItem *editItem = [[NSMenuItem alloc] init];
  [menuBar addItem:editItem];
  NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
  [editMenu addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
  [editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
  [editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
  [editMenu addItemWithTitle:@"Select All"
                      action:@selector(selectAll:)
               keyEquivalent:@"a"];
  [editItem setSubmenu:editMenu];
}

// YES while the menu bar is showing SF Symbols rather than bundled art. Only
// a template image responds to a tint, so the remote-mode tint below is
// applied only in that case.
static BOOL gIconsAreTemplate = NO;

// name is a bundled PNG; fallbackSymbol is the SF Symbol to use when it is
// missing.
//
// Bundled art is rendered in colour, deliberately not as a template. A
// template image is drawn from its alpha channel alone, which throws the
// colour away — and the two glyphs here differ only by the colour of their
// status dot, so as templates they would be indistinguishable. The trade is
// that the art has to be legible on both light and dark menu bars by itself,
// which a mid-grey outline with a saturated dot is.
static NSImage *loadStatusImage(NSString *name, NSString *fallbackSymbol) {
  NSString *path = [[NSBundle mainBundle] pathForResource:name ofType:@"png"];
  if (path) {
    NSImage *image = [[NSImage alloc] initWithContentsOfFile:path];
    if (image) {
      [image setSize:NSMakeSize(18, 18)];
      [image setTemplate:NO];
      return image;
    }
  }
  gIconsAreTemplate = YES;
  // No bundled art — either the bare binary is being run rather than the .app,
  // or no glyph has been drawn yet. A system symbol is a real template image,
  // so this looks native rather than wrong.
  if (@available(macOS 11.0, *)) {
    return [NSImage imageWithSystemSymbolName:fallbackSymbol
                     accessibilityDescription:@"ESP HID Bridge"];
  }
  return nil;
}

static void buildStatusItem(void) {
  gStatusItem = [[NSStatusBar systemStatusBar]
      statusItemWithLength:NSSquareStatusItemLength];
  gIconIdle = loadStatusImage(@"status-idle", @"dot.radiowaves.left.and.right");
  gIconActive =
      loadStatusImage(@"status-active", @"antenna.radiowaves.left.and.right");
  if (gIconIdle) {
    [[gStatusItem button] setImage:gIconIdle];
  } else {
    [[gStatusItem button] setTitle:@"HID"];
  }
  [[gStatusItem button] setToolTip:@"ESP HID Bridge"];

  NSMenu *menu = [[NSMenu alloc] init];
  NSMenuItem *open = [[NSMenuItem alloc] initWithTitle:@"Open"
                                                action:@selector(openWindow:)
                                         keyEquivalent:@""];
  [open setTarget:gController];
  [menu addItem:open];
  [menu addItem:[NSMenuItem separatorItem]];
  [menu addItemWithTitle:@"Quit" action:@selector(terminate:) keyEquivalent:@""];
  [gStatusItem setMenu:menu];
}

static void buildWindow(void) {
  NSRect frame = NSMakeRect(0, 0, kWindowWidth, kWindowHeight);
  gWindow = [[NSWindow alloc]
      initWithContentRect:frame
                styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                          NSWindowStyleMaskMiniaturizable
                  backing:NSBackingStoreBuffered
                    defer:NO];
  [gWindow setTitle:@"ESP HID Bridge"];
  [gWindow setDelegate:gController];
  [gWindow center];

  NSView *root = [gWindow contentView];

  // --- Connection & Status -----------------------------------------------
  NSBox *statusBox = makeBox(root, @"Connection & Status", 275, 265);
  NSView *sv = [statusBox contentView];
  CGFloat sw = NSWidth([sv bounds]);

  gBanner = makeLabel(sv, @"", 0, 205, sw, YES);
  [gBanner setTextColor:[NSColor systemRedColor]];
  [gBanner setHidden:YES];

  gGrantButton = makeButton(sv, @"Grant Permission…", 0, 175, 170,
                            @selector(grantClicked:));
  gSettingsButton = makeButton(sv, @"Open System Settings", 180, 175, 190,
                               @selector(settingsClicked:));
  [gGrantButton setHidden:YES];
  [gSettingsButton setHidden:YES];

  const CGFloat labelWidth = 90;
  const CGFloat valueX = 100;
  makeLabel(sv, @"Bridge:", 0, 140, labelWidth, NO);
  gStatusBridge = makeValue(sv, @"Stopped", valueX, 140, sw - valueX);
  makeLabel(sv, @"Device:", 0, 113, labelWidth, NO);
  gStatusDevice = makeValue(sv, @"-", valueX, 113, sw - valueX);
  makeLabel(sv, @"Firmware:", 0, 86, labelWidth, NO);
  gStatusFirmware = makeValue(sv, @"-", valueX, 86, sw - valueX);
  makeLabel(sv, @"Bluetooth:", 0, 59, labelWidth, NO);
  gStatusBluetooth = makeValue(sv, @"-", valueX, 59, sw - valueX);

  gStartButton = makeButton(sv, @"Start", 0, 16, 100, @selector(startClicked:));
  gStopButton = makeButton(sv, @"Stop", 110, 16, 100, @selector(stopClicked:));
  gBondsButton = makeButton(sv, @"Clear device bonds", sw - 190, 16, 190,
                            @selector(bondsClicked:));
  [gStopButton setEnabled:NO];

  // --- Input Settings -----------------------------------------------------
  NSBox *inputBox = makeBox(root, @"Input Settings", 135, 130);
  NSView *iv = [inputBox contentView];
  CGFloat iw = NSWidth([iv bounds]);

  makeLabel(iv, @"Toggle hotkey:", 0, 72, 110, NO);
  gHotkeyField = makeField(iv, 115, 70, 150);
  [gHotkeyField setToolTip:@"For example: F9, or Ctrl+Alt+F7"];

  makeLabel(iv, @"Send rate (Hz):", iw - 250, 72, 110, NO);
  gRateField = makeField(iv, iw - 135, 70, 70);

  gKeyboardCheck = [[NSButton alloc] initWithFrame:NSMakeRect(0, 32, 200, 22)];
  [gKeyboardCheck setButtonType:NSButtonTypeSwitch];
  [gKeyboardCheck setTitle:@"Forward keyboard"];
  [iv addSubview:gKeyboardCheck];

  // No Auto/Manual choice here: switching to the device is hotkey-only on
  // macOS. Coming back is still automatic, so say so rather than leaving the
  // user to work out which directions are on offer.
  NSTextField *hint = makeLabel(
      iv, @"Switch with the hotkey; push past the far edge to come back.",
      iw - 400, 32, 400, NO);
  [hint setAlignment:NSTextAlignmentRight];
  [hint setTextColor:[NSColor secondaryLabelColor]];
  [hint setFont:[NSFont systemFontOfSize:11]];

  // --- Device Layout ------------------------------------------------------
  NSBox *layoutBox = makeBox(root, @"Device Layout", 20, 95);
  NSView *lv = [layoutBox contentView];
  CGFloat lw = NSWidth([lv bounds]);

  makeLabel(lv, @"Device resolution:", 0, 32, 130, NO);
  gResolutionCombo = [[NSComboBox alloc]
      initWithFrame:NSMakeRect(135, 30, 150, 24)];
  [gResolutionCombo setEditable:YES];
  [lv addSubview:gResolutionCombo];

  makeLabel(lv, @"This Mac sits:", lw - 250, 32, 110, NO);
  gHostSidePopup = [[NSPopUpButton alloc]
      initWithFrame:NSMakeRect(lw - 135, 30, 135, 25)];
  [lv addSubview:gHostSidePopup];
}

void ehbGuiInit(void) {
  [NSApplication sharedApplication];
  // Without a Regular activation policy a non-bundled binary gets no Dock
  // tile, cannot become key, and never shows its window — the classic
  // "it runs but nothing appears" symptom.
  [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

  gController = [[EHBController alloc] init];
  [NSApp setDelegate:gController];

  buildMenuBar();
  buildWindow();
  buildStatusItem();

  [NSTimer scheduledTimerWithTimeInterval:1.0
                                   target:gController
                                 selector:@selector(tick:)
                                 userInfo:nil
                                  repeats:YES];
}

void ehbGuiRun(void) {
  [gWindow makeKeyAndOrderFront:nil];
  [NSApp activateIgnoringOtherApps:YES];
  [NSApp run];
}

void ehbGuiTerminate(void) { [NSApp terminate:nil]; }

void ehbGuiAddResolution(const char *value) {
  [gResolutionCombo addItemWithObjectValue:[NSString stringWithUTF8String:value]];
}

void ehbGuiAddHostSide(const char *value) {
  [gHostSidePopup addItemWithTitle:[NSString stringWithUTF8String:value]];
}

void ehbGuiSetForm(const char *hotkey, int rateHz, int captureKeyboard,
                   const char *resolution, int hostSideIndex) {
  [gHotkeyField setStringValue:[NSString stringWithUTF8String:hotkey]];
  [gRateField setStringValue:[NSString stringWithFormat:@"%d", rateHz]];
  [gKeyboardCheck setState:captureKeyboard ? NSControlStateValueOn
                                           : NSControlStateValueOff];
  [gResolutionCombo setStringValue:[NSString stringWithUTF8String:resolution]];
  if (hostSideIndex >= 0 && hostSideIndex < [gHostSidePopup numberOfItems]) {
    [gHostSidePopup selectItemAtIndex:hostSideIndex];
  }
}

EhbForm ehbGuiReadForm(void) {
  EhbForm form;
  memset(&form, 0, sizeof(form));
  strncpy(form.hotkey, [[gHotkeyField stringValue] UTF8String],
          sizeof(form.hotkey) - 1);
  strncpy(form.resolution, [[gResolutionCombo stringValue] UTF8String],
          sizeof(form.resolution) - 1);
  form.rateHz = [gRateField intValue];
  form.captureKeyboard = ([gKeyboardCheck state] == NSControlStateValueOn) ? 1 : 0;
  form.hostSideIndex = (int)[gHostSidePopup indexOfSelectedItem];
  return form;
}

void ehbGuiSetStatus(const char *bridge, const char *device,
                     const char *firmware, const char *bluetooth) {
  if (bridge) {
    [gStatusBridge setStringValue:[NSString stringWithUTF8String:bridge]];
  }
  if (device) {
    [gStatusDevice setStringValue:[NSString stringWithUTF8String:device]];
  }
  if (firmware) {
    [gStatusFirmware setStringValue:[NSString stringWithUTF8String:firmware]];
  }
  if (bluetooth) {
    [gStatusBluetooth setStringValue:[NSString stringWithUTF8String:bluetooth]];
  }
}

void ehbGuiSetRunning(int running) {
  [gStartButton setEnabled:running ? NO : YES];
  [gStopButton setEnabled:running ? YES : NO];
  // Settings are captured when the bridge starts, so they are locked while
  // it runs rather than silently having no effect.
  [gHotkeyField setEnabled:running ? NO : YES];
  [gRateField setEnabled:running ? NO : YES];
  [gKeyboardCheck setEnabled:running ? NO : YES];
  [gResolutionCombo setEnabled:running ? NO : YES];
  [gHostSidePopup setEnabled:running ? NO : YES];
}

void ehbGuiSetRemoteActive(int active) {
  NSImage *image = active ? gIconActive : gIconIdle;
  if (image) {
    [[gStatusItem button] setImage:image];
  }
  // The bundled art carries the state in its own colours. The SF Symbol
  // fallback cannot — a template image is drawn from its alpha channel alone —
  // so in that case tint it instead, the way the system's own menu bar items
  // signal activity.
  if (gIconsAreTemplate) {
    [[gStatusItem button]
        setContentTintColor:active ? [NSColor controlAccentColor] : nil];
  }
  [[gStatusItem button]
      setToolTip:active ? @"ESP HID Bridge — input going to the device"
                        : @"ESP HID Bridge"];
}

void ehbGuiSetBanner(const char *message, int visible, int showGrantButtons) {
  if (message) {
    [gBanner setStringValue:[NSString stringWithUTF8String:message]];
  }
  [gBanner setHidden:visible ? NO : YES];
  [gGrantButton setHidden:(visible && showGrantButtons) ? NO : YES];
  [gSettingsButton setHidden:(visible && showGrantButtons) ? NO : YES];
}

void ehbGuiShowAlert(const char *title, const char *message, int isError) {
  NSAlert *alert = [[NSAlert alloc] init];
  [alert setMessageText:[NSString stringWithUTF8String:title]];
  [alert setInformativeText:[NSString stringWithUTF8String:message]];
  [alert setAlertStyle:isError ? NSAlertStyleCritical : NSAlertStyleInformational];
  [alert addButtonWithTitle:@"OK"];
  [alert runModal];
}

void ehbGuiOpenPrivacySettings(const char *anchor) {
  NSString *url = [NSString
      stringWithFormat:@"x-apple.systempreferences:com.apple.preference.security?%s",
                       anchor];
  [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:url]];
}

void ehbGuiPerformOnMain(uintptr_t token) {
  dispatch_async(dispatch_get_main_queue(), ^{
    goGuiPerform(token);
  });
}

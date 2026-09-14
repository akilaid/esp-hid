#import <Cocoa/Cocoa.h>

#include "gui_darwin.h"

// Implemented in Go (gui_darwin.go).
extern void goGuiStartClicked(void);
extern void goGuiStopClicked(void);
extern void goGuiClearBondsClicked(void);
extern void goGuiForgetDeviceClicked(void);
extern void goGuiGrantClicked(void);
extern void goGuiOpenSettingsClicked(void);
extern void goGuiTick(void);
extern void goGuiWillTerminate(void);
extern void goGuiPerform(uintptr_t token);
// Device-layout edits. Signatures match what cgo exports for *C.char / C.int.
extern void goGuiDeviceSearchChanged(char *text);
extern void goGuiDeviceMatchSelected(int index);
extern void goGuiOrientationChanged(int index);
extern void goGuiResolutionEdited(char *text);

// Fixed-size window: the layout is hand-placed, which is a fair trade for a
// settings form that never needs to resize and keeps this file free of
// constraint plumbing.
static const CGFloat kWindowWidth = 620;
static const CGFloat kWindowHeight = 635;
static const CGFloat kMargin = 20;
static const CGFloat kRowHeight = 22;

@interface EHBController
    : NSObject <NSApplicationDelegate, NSWindowDelegate, NSComboBoxDelegate,
                NSSearchFieldDelegate, NSTableViewDataSource, NSTableViewDelegate>
@end

// The list under the Device field, in the style of the search in System
// Settings: a borderless child window that never becomes key, so the field
// keeps the focus and the keyboard while the list is up. Up/Down move the
// highlight, Return or a click picks, Escape puts it away.
@interface EHBSuggestionTable : NSTableView
@end

@implementation EHBSuggestionTable
// The window is never key, and a click into a non-key window would otherwise
// only activate it; this lets the first click land on a row.
- (BOOL)acceptsFirstMouse:(NSEvent *)event {
  (void)event;
  return YES;
}
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
static NSButton *gForgetButton = nil;

static NSTextField *gHotkeyField = nil;
static NSTextField *gRateField = nil;
static NSButton *gKeyboardCheck = nil;
static NSSegmentedControl *gModeControl = nil;
static NSComboBox *gResolutionCombo = nil;
static NSPopUpButton *gHostSidePopup = nil;
static NSSearchField *gDeviceSearch = nil;
static NSPopUpButton *gOrientationPopup = nil;

static NSWindow *gSuggestions = nil;
static NSScrollView *gSuggestionScroll = nil;
static EHBSuggestionTable *gSuggestionTable = nil;
static NSMutableArray<NSString *> *gSuggestionLabels = nil;
static NSMutableArray<NSString *> *gSuggestionNames = nil;
static const CGFloat kSuggestionRowHeight = 24;
static const CGFloat kSuggestionPad = 5;
static const NSUInteger kSuggestionRowsShown = 8;
static NSTextField *gResolutionHint = nil;

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

static void hideSuggestions(void) {
  if ([gSuggestions parentWindow]) {
    [gWindow removeChildWindow:gSuggestions];
  }
  [gSuggestions orderOut:nil];
}

static void showSuggestions(void) {
  NSUInteger count = [gSuggestionLabels count];
  if (count == 0 || ![gWindow isVisible]) {
    hideSuggestions();
    return;
  }
  NSRect field = [gDeviceSearch convertRect:[gDeviceSearch bounds] toView:nil];
  NSRect anchor = [gWindow convertRectToScreen:field];
  CGFloat rows = (CGFloat)MIN(count, kSuggestionRowsShown);
  CGFloat height = rows * kSuggestionRowHeight + 2 * kSuggestionPad;
  NSRect frame = NSMakeRect(NSMinX(anchor), NSMinY(anchor) - height - 4,
                            NSWidth(anchor), height);
  [gSuggestions setFrame:frame display:NO];

  // Laid out by hand on every show rather than left to autoresizing: the
  // panel goes from a handful of rows to one and back, and a stale frame or
  // scroll offset from the previous size is exactly what clips a lone row.
  NSRect inner = NSInsetRect([[gSuggestions contentView] bounds], 0, kSuggestionPad);
  [gSuggestionScroll setFrame:inner];
  [gSuggestionTable reloadData];
  [gSuggestionTable setFrameSize:NSMakeSize(NSWidth(inner), rows * kSuggestionRowHeight)];
  [gSuggestionTable sizeLastColumnToFit];
  [[gSuggestionScroll contentView] scrollToPoint:NSZeroPoint];
  [gSuggestionScroll reflectScrolledClipView:[gSuggestionScroll contentView]];

  if ([gSuggestions parentWindow] == nil) {
    [gWindow addChildWindow:gSuggestions ordered:NSWindowAbove];
  }
  [gSuggestions orderFront:nil];
}

static void selectSuggestion(NSInteger row) {
  if (row < 0 || row >= (NSInteger)[gSuggestionLabels count]) {
    return;
  }
  [gSuggestionTable selectRowIndexes:[NSIndexSet indexSetWithIndex:(NSUInteger)row]
                byExtendingSelection:NO];
  [gSuggestionTable scrollRowToVisible:row];
}

// Return or a click: commit the row, leave its name in the field, put the
// list away. The highlight already filled the resolution as it moved.
static void acceptSuggestion(NSInteger row) {
  if (row >= 0 && row < (NSInteger)[gSuggestionNames count]) {
    goGuiDeviceMatchSelected((int)row);
    [gDeviceSearch setStringValue:gSuggestionNames[(NSUInteger)row]];
  }
  hideSuggestions();
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

- (void)forgetClicked:(id)sender {
  (void)sender;
  goGuiForgetDeviceClicked();
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

// The search field's action fires on Return and on its clear button, not per
// keystroke (typing goes through controlTextDidChange:). Only the cleared
// case needs handling — it is the one edit the delegate does not see — and
// Return must not re-run the search, or it would undo a pick made from the
// popup.
- (void)deviceSearchAction:(id)sender {
  (void)sender;
  if ([[gDeviceSearch stringValue] length] == 0) {
    goGuiDeviceSearchChanged((char *)"");
  }
}

- (void)suggestionClicked:(id)sender {
  (void)sender;
  acceptSuggestion([gSuggestionTable clickedRow]);
}

// Moving the highlight fills the resolution, so what the field shows below
// is always the row that is lit.
- (void)tableViewSelectionDidChange:(NSNotification *)note {
  (void)note;
  NSInteger row = [gSuggestionTable selectedRow];
  if (row >= 0) {
    goGuiDeviceMatchSelected((int)row);
  }
}

- (NSInteger)numberOfRowsInTableView:(NSTableView *)table {
  (void)table;
  return (NSInteger)[gSuggestionLabels count];
}

- (NSView *)tableView:(NSTableView *)table
    viewForTableColumn:(NSTableColumn *)column
                   row:(NSInteger)row {
  (void)column;
  NSTableCellView *cell = [table makeViewWithIdentifier:@"label" owner:nil];
  if (cell == nil) {
    cell = [[NSTableCellView alloc]
        initWithFrame:NSMakeRect(0, 0, 200, kSuggestionRowHeight)];
    [cell setIdentifier:@"label"];
    NSTextField *text = [[NSTextField alloc] initWithFrame:NSMakeRect(8, 3, 184, 18)];
    [text setAutoresizingMask:NSViewWidthSizable];
    [text setBezeled:NO];
    [text setDrawsBackground:NO];
    [text setEditable:NO];
    [text setSelectable:NO];
    [text setLineBreakMode:NSLineBreakByTruncatingTail];
    [text setFont:[NSFont systemFontOfSize:13]];
    [cell addSubview:text];
    // Registered as the cell's text field so the highlight recolours it.
    [cell setTextField:text];
  }
  [[cell textField] setStringValue:gSuggestionLabels[(NSUInteger)row]];
  return cell;
}

// Keyboard for the list while the search field keeps focus. Anything not
// handled here falls through to the field's own behaviour — including
// Escape when the list is already down, which clears the search.
- (BOOL)control:(NSControl *)control
               textView:(NSTextView *)textView
    doCommandBySelector:(SEL)command {
  (void)textView;
  if (control != gDeviceSearch) {
    return NO;
  }
  BOOL shown = [gSuggestions isVisible];
  NSInteger rows = (NSInteger)[gSuggestionLabels count];
  if (command == @selector(moveDown:)) {
    if (!shown) {
      showSuggestions();
      selectSuggestion(0);
    } else {
      selectSuggestion(MIN([gSuggestionTable selectedRow] + 1, rows - 1));
    }
    return YES;
  }
  if (command == @selector(moveUp:) && shown) {
    selectSuggestion(MAX([gSuggestionTable selectedRow] - 1, 0));
    return YES;
  }
  if (command == @selector(insertNewline:) && shown) {
    acceptSuggestion([gSuggestionTable selectedRow]);
    return YES;
  }
  if (command == @selector(cancelOperation:) && shown) {
    hideSuggestions();
    return YES;
  }
  return NO;
}

// Focus leaving the field takes the list with it. A click on a row does not
// count: the list's window is never key, so the field stays first responder.
- (void)controlTextDidEndEditing:(NSNotification *)note {
  if ([note object] == gDeviceSearch) {
    hideSuggestions();
  }
}

- (void)windowDidResignKey:(NSNotification *)note {
  if ([note object] == gWindow) {
    hideSuggestions();
  }
}

- (void)orientationChanged:(id)sender {
  (void)sender;
  goGuiOrientationChanged((int)[gOrientationPopup indexOfSelectedItem]);
}

// Typing, in either the device search or the resolution field. Only user
// edits arrive here — setStringValue: from the ehbGui* setters does not —
// so nothing written by Go comes back round as an edit.
- (void)controlTextDidChange:(NSNotification *)note {
  id object = [note object];
  if (object == gDeviceSearch) {
    goGuiDeviceSearchChanged((char *)[[gDeviceSearch stringValue] UTF8String]);
  } else if (object == gResolutionCombo) {
    goGuiResolutionEdited((char *)[[gResolutionCombo stringValue] UTF8String]);
  }
}

// A preset picked from the resolution combo's list. stringValue still holds
// the previous text at this point, so read the chosen item instead.
- (void)comboBoxSelectionDidChange:(NSNotification *)note {
  if ([note object] != gResolutionCombo) {
    return;
  }
  NSInteger index = [gResolutionCombo indexOfSelectedItem];
  if (index < 0) {
    return;
  }
  NSString *value = [gResolutionCombo itemObjectValueAtIndex:index];
  goGuiResolutionEdited((char *)[value UTF8String]);
}

- (void)tick:(NSTimer *)timer {
  (void)timer;
  goGuiTick();
}

// Closing the window hides it, matching the Windows build where the X button
// minimises to the tray. Quitting is done from the menu bar.
- (BOOL)windowShouldClose:(NSWindow *)sender {
  hideSuggestions();
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
  // hotkey, device search and resolution fields.
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

static void buildSuggestions(void) {
  gSuggestionLabels = [NSMutableArray array];
  gSuggestionNames = [NSMutableArray array];

  gSuggestions = [[NSWindow alloc]
      initWithContentRect:NSMakeRect(0, 0, 300, 100)
                styleMask:NSWindowStyleMaskBorderless
                  backing:NSBackingStoreBuffered
                    defer:NO];
  [gSuggestions setOpaque:NO];
  [gSuggestions setBackgroundColor:[NSColor clearColor]];
  [gSuggestions setHasShadow:YES];
  [gSuggestions setReleasedWhenClosed:NO];

  // The menu material gives the same translucent, rounded panel the
  // system's own completion lists use, in both appearances.
  NSVisualEffectView *backdrop = [[NSVisualEffectView alloc]
      initWithFrame:[[gSuggestions contentView] bounds]];
  [backdrop setMaterial:NSVisualEffectMaterialMenu];
  [backdrop setBlendingMode:NSVisualEffectBlendingModeBehindWindow];
  [backdrop setState:NSVisualEffectStateActive];
  [backdrop setWantsLayer:YES];
  [[backdrop layer] setCornerRadius:8];
  [[backdrop layer] setMasksToBounds:YES];
  [gSuggestions setContentView:backdrop];

  // Frames are set on every show (see showSuggestions), so nothing here
  // autoresizes and the scroll view is told not to invent insets of its own.
  NSScrollView *scroll = [[NSScrollView alloc]
      initWithFrame:NSInsetRect([backdrop bounds], 0, kSuggestionPad)];
  [scroll setBorderType:NSNoBorder];
  [scroll setDrawsBackground:NO];
  [scroll setHasVerticalScroller:YES];
  [scroll setAutohidesScrollers:YES];
  [scroll setAutomaticallyAdjustsContentInsets:NO];
  [scroll setContentInsets:NSEdgeInsetsMake(0, 0, 0, 0)];
  gSuggestionScroll = scroll;

  gSuggestionTable = [[EHBSuggestionTable alloc]
      initWithFrame:[[scroll contentView] bounds]];
  // Plain, not the macOS 11 inset style, whose rounded padded rows assume a
  // sidebar-sized table and would leave a lone row swimming in margin.
  [gSuggestionTable setStyle:NSTableViewStylePlain];
  NSTableColumn *column = [[NSTableColumn alloc] initWithIdentifier:@"label"];
  [column setResizingMask:NSTableColumnAutoresizingMask];
  [gSuggestionTable addTableColumn:column];
  [gSuggestionTable setHeaderView:nil];
  [gSuggestionTable setRowHeight:kSuggestionRowHeight];
  [gSuggestionTable setIntercellSpacing:NSMakeSize(0, 0)];
  [gSuggestionTable
      setColumnAutoresizingStyle:NSTableViewLastColumnOnlyAutoresizingStyle];
  [gSuggestionTable setBackgroundColor:[NSColor clearColor]];
  [gSuggestionTable setDataSource:gController];
  [gSuggestionTable setDelegate:gController];
  [gSuggestionTable setTarget:gController];
  [gSuggestionTable setAction:@selector(suggestionClicked:)];
  [scroll setDocumentView:gSuggestionTable];
  [backdrop addSubview:scroll];
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
  NSBox *statusBox = makeBox(root, @"Connection & Status", 350, 265);
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
  // Both device-maintenance actions share the row's right-hand side. Bonds is
  // narrowed to 170 so "Forget device" clears the Stop button, which ends at
  // x=210; the layout here is hand-placed absolute frames.
  gBondsButton = makeButton(sv, @"Clear device bonds", sw - 170, 16, 170,
                            @selector(bondsClicked:));
  gForgetButton = makeButton(sv, @"Forget device", sw - 330, 16, 150,
                             @selector(forgetClicked:));
  [gStopButton setEnabled:NO];

  // --- Input Settings -----------------------------------------------------
  NSBox *inputBox = makeBox(root, @"Input Settings", 210, 130);
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

  makeLabel(iv, @"Switching:", iw - 250, 32, 110, NO);
  gModeControl = [[NSSegmentedControl alloc]
      initWithFrame:NSMakeRect(iw - 135, 30, 135, 24)];
  [gModeControl setSegmentCount:2];
  [gModeControl setLabel:@"Auto" forSegment:0];
  [gModeControl setLabel:@"Manual" forSegment:1];
  [gModeControl setSegmentStyle:NSSegmentStyleRounded];
  // Auto is deliberately not a light touch on macOS: a single-display Mac puts
  // the Dock, the menu bar and every close button on the same borders, so the
  // pointer has to be pushed against the edge rather than merely reach it.
  [gModeControl
      setToolTip:@"Auto: push the pointer against the screen edge to switch. "
                 @"Either way, push past the far edge of the device's screen "
                 @"to come back."];
  [iv addSubview:gModeControl];

  // --- Device Layout ------------------------------------------------------
  NSBox *layoutBox = makeBox(root, @"Device Layout", 20, 170);
  NSView *lv = [layoutBox contentView];
  CGFloat lw = NSWidth([lv bounds]);

  // Row 1: find the device by name. Matches drop down under the field as
  // the user types, and the best one fills the resolution straight away.
  makeLabel(lv, @"Device:", 0, 110, 130, NO);
  gDeviceSearch = [[NSSearchField alloc]
      initWithFrame:NSMakeRect(135, 108, lw - 135, 24)];
  [gDeviceSearch setPlaceholderString:@"Phone or tablet name"];
  [gDeviceSearch setDelegate:gController];
  [gDeviceSearch setTarget:gController];
  [gDeviceSearch setAction:@selector(deviceSearchAction:)];
  [lv addSubview:gDeviceSearch];

  // Row 2: the resolution itself — what is actually saved — and its
  // orientation, which is just the same two numbers the other way round.
  makeLabel(lv, @"Device resolution:", 0, 76, 130, NO);
  gResolutionCombo = [[NSComboBox alloc]
      initWithFrame:NSMakeRect(135, 74, 150, 24)];
  [gResolutionCombo setEditable:YES];
  [gResolutionCombo setDelegate:gController];
  [lv addSubview:gResolutionCombo];

  makeLabel(lv, @"Orientation:", lw - 250, 76, 110, NO);
  gOrientationPopup = [[NSPopUpButton alloc]
      initWithFrame:NSMakeRect(lw - 135, 74, 135, 25)];
  [gOrientationPopup setTarget:gController];
  [gOrientationPopup setAction:@selector(orientationChanged:)];
  [lv addSubview:gOrientationPopup];

  // Row 3.
  makeLabel(lv, @"This Mac sits:", 0, 42, 130, NO);
  gHostSidePopup = [[NSPopUpButton alloc]
      initWithFrame:NSMakeRect(135, 40, 150, 25)];
  [lv addSubview:gHostSidePopup];

  // Text comes from Go (ui.ResolutionHint) so both GUIs say the same thing.
  gResolutionHint = makeLabel(lv, @"", 0, 8, lw, NO);
  [gResolutionHint setFont:[NSFont systemFontOfSize:11]];
  [gResolutionHint setTextColor:[NSColor secondaryLabelColor]];
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
  buildSuggestions();
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

void ehbGuiAddOrientation(const char *value) {
  [gOrientationPopup addItemWithTitle:[NSString stringWithUTF8String:value]];
}

void ehbGuiClearDeviceMatches(void) {
  [gSuggestionLabels removeAllObjects];
  [gSuggestionNames removeAllObjects];
}

void ehbGuiAddDeviceMatch(const char *label, const char *name) {
  [gSuggestionLabels addObject:[NSString stringWithUTF8String:label]];
  [gSuggestionNames addObject:[NSString stringWithUTF8String:name]];
}

void ehbGuiShowDeviceMatches(void) { showSuggestions(); }

void ehbGuiSelectDeviceMatch(int index) { selectSuggestion(index); }

void ehbGuiSetResolution(const char *value) {
  [gResolutionCombo setStringValue:[NSString stringWithUTF8String:value]];
}

void ehbGuiSetResolutionHint(const char *text) {
  NSString *value = [NSString stringWithUTF8String:text];
  [gResolutionHint setStringValue:value];
  [gResolutionCombo setToolTip:value];
}

void ehbGuiSetOrientation(int index) {
  if (index >= 0 && index < [gOrientationPopup numberOfItems]) {
    [gOrientationPopup selectItemAtIndex:index];
  }
}

void ehbGuiSetForm(const char *hotkey, int rateHz, int captureKeyboard,
                   int autoSwitch, const char *resolution, int hostSideIndex) {
  [gHotkeyField setStringValue:[NSString stringWithUTF8String:hotkey]];
  [gRateField setStringValue:[NSString stringWithFormat:@"%d", rateHz]];
  [gKeyboardCheck setState:captureKeyboard ? NSControlStateValueOn
                                           : NSControlStateValueOff];
  [gModeControl setSelectedSegment:autoSwitch ? 0 : 1];
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
  form.autoSwitch = ([gModeControl selectedSegment] == 0) ? 1 : 0;
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
  [gModeControl setEnabled:running ? NO : YES];
  [gResolutionCombo setEnabled:running ? NO : YES];
  [gHostSidePopup setEnabled:running ? NO : YES];
  [gDeviceSearch setEnabled:running ? NO : YES];
  [gOrientationPopup setEnabled:running ? NO : YES];
  if (running) {
    hideSuggestions();
  }
  // Forgetting the bound board mid-session would leave the running link
  // pointing at a device the settings no longer name.
  [gForgetButton setEnabled:running ? NO : YES];
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

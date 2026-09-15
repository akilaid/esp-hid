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
extern void goGuiUpdateClicked(void);
extern void goGuiCheckUpdatesClicked(void);
extern void goGuiToggleAutoUpdatesClicked(void);
// Arrangement picture: phase 0 = mouse down, 1 = drag, 2 = mouse up, in the
// view's coordinates (y down).
extern void goGuiArrangeMouse(int phase, double x, double y);
extern void goGuiDisplaysChanged(void);

// Fixed-size window: the layout is hand-placed, which is a fair trade for a
// settings form that never needs to resize and keeps this file free of
// constraint plumbing.
static const CGFloat kWindowWidth = 620;
static const CGFloat kWindowHeight = 781;
static const CGFloat kMargin = 20;
static const CGFloat kRowHeight = 22;
// The status box is tallest with the permission banner and its buttons on
// top. Without them that strip is dead space, so the box and the window give
// it up; every other frame is anchored to the bottom and stays put.
static const CGFloat kStatusBoxHeight = 301;
static const CGFloat kBannerStripHeight = 65;
// The arrangement picture inside the Device Layout box.
static const CGFloat kArrangeHeight = 130;

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

// The display-arrangement picture. Everything it shows arrives from Go
// already laid out in this view's coordinates; it paints, tracks the mouse,
// and hands the events straight back. Flipped so Go's y-down rectangles map
// without conversion.
@interface EHBArrangeView : NSView
@property(nonatomic, strong) NSMutableArray<NSValue *> *displayRects;
@property(nonatomic, strong) NSMutableArray<NSString *> *displayNames;
@property(nonatomic, strong) NSMutableArray<NSNumber *> *displayPrimary;
@property(nonatomic) NSRect deviceRect;
@property(nonatomic, strong) NSString *deviceLabel;
@property(nonatomic) BOOL dragging;
@property(nonatomic) BOOL enabled;
@end

@implementation EHBArrangeView

- (instancetype)initWithFrame:(NSRect)frame {
  self = [super initWithFrame:frame];
  if (self) {
    _displayRects = [NSMutableArray array];
    _displayNames = [NSMutableArray array];
    _displayPrimary = [NSMutableArray array];
    _deviceLabel = @"";
    _enabled = YES;
    [self setWantsLayer:YES];
    [[self layer] setCornerRadius:8];
    [[self layer] setMasksToBounds:YES];
  }
  return self;
}

- (BOOL)isFlipped {
  return YES;
}

- (void)drawRect:(NSRect)dirty {
  (void)dirty;
  CGFloat alpha = self.enabled ? 1.0 : 0.4;

  // Backdrop: a quiet well the desktop sits in.
  [[[NSColor labelColor] colorWithAlphaComponent:0.06] setFill];
  NSRectFill([self bounds]);

  NSMutableParagraphStyle *centred = [[NSMutableParagraphStyle alloc] init];
  [centred setAlignment:NSTextAlignmentCenter];
  [centred setLineBreakMode:NSLineBreakByTruncatingTail];

  for (NSUInteger i = 0; i < [self.displayRects count]; i++) {
    NSRect r = [self.displayRects[i] rectValue];
    BOOL primary = [self.displayPrimary[i] boolValue];
    NSBezierPath *path = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(r, 0.5, 0.5)
                                                         xRadius:3
                                                         yRadius:3];
    [[[NSColor controlAccentColor] colorWithAlphaComponent:0.55 * alpha] setFill];
    [path fill];
    [[[NSColor controlAccentColor] colorWithAlphaComponent:alpha] setStroke];
    [path setLineWidth:1];
    [path stroke];
    if (primary && NSHeight(r) > 12) {
      // The menu bar, the way System Settings marks the main display.
      [[[NSColor whiteColor] colorWithAlphaComponent:0.75 * alpha] setFill];
      NSRectFill(NSMakeRect(NSMinX(r) + 1, NSMinY(r) + 1, NSWidth(r) - 2, 3));
    }
    if (NSWidth(r) > 40 && NSHeight(r) > 20) {
      NSDictionary *attrs = @{
        NSFontAttributeName : [NSFont systemFontOfSize:10],
        NSForegroundColorAttributeName : [[NSColor whiteColor] colorWithAlphaComponent:alpha],
        NSParagraphStyleAttributeName : centred,
      };
      NSRect textRect = NSInsetRect(r, 4, 0);
      textRect.origin.y = NSMidY(r) - 7;
      textRect.size.height = 14;
      [self.displayNames[i] drawInRect:textRect withAttributes:attrs];
    }
  }

  NSRect d = self.deviceRect;
  NSBezierPath *device = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(d, 0.5, 0.5)
                                                         xRadius:3
                                                         yRadius:3];
  NSColor *tint = [NSColor systemOrangeColor];
  [[tint colorWithAlphaComponent:(self.dragging ? 0.85 : 0.65) * alpha] setFill];
  [device fill];
  [[tint colorWithAlphaComponent:alpha] setStroke];
  [device setLineWidth:self.dragging ? 2 : 1];
  [device stroke];
  if (NSWidth(d) > 44 && NSHeight(d) > 14) {
    NSDictionary *attrs = @{
      NSFontAttributeName : [NSFont systemFontOfSize:9],
      NSForegroundColorAttributeName : [[NSColor whiteColor] colorWithAlphaComponent:alpha],
      NSParagraphStyleAttributeName : centred,
    };
    NSRect textRect = NSInsetRect(d, 2, 0);
    textRect.origin.y = NSMidY(d) - 6;
    textRect.size.height = 12;
    [self.deviceLabel drawInRect:textRect withAttributes:attrs];
  }
}

- (void)resetCursorRects {
  if (self.enabled && !NSIsEmptyRect(self.deviceRect)) {
    [self addCursorRect:self.deviceRect cursor:[NSCursor openHandCursor]];
  }
}

- (void)mouseDown:(NSEvent *)event {
  if (!self.enabled) {
    return;
  }
  NSPoint p = [self convertPoint:[event locationInWindow] fromView:nil];
  goGuiArrangeMouse(0, p.x, p.y);
}

- (void)mouseDragged:(NSEvent *)event {
  if (!self.enabled) {
    return;
  }
  NSPoint p = [self convertPoint:[event locationInWindow] fromView:nil];
  goGuiArrangeMouse(1, p.x, p.y);
}

- (void)mouseUp:(NSEvent *)event {
  if (!self.enabled) {
    return;
  }
  NSPoint p = [self convertPoint:[event locationInWindow] fromView:nil];
  goGuiArrangeMouse(2, p.x, p.y);
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

static NSBox *gStatusBox = nil;
static NSTextField *gBanner = nil;
static NSButton *gGrantButton = nil;
static NSButton *gSettingsButton = nil;
static NSButton *gUpdateButton = nil;
static NSMenuItem *gAutoUpdateItem = nil;

static NSButton *gStartButton = nil;
static NSButton *gStopButton = nil;
static NSButton *gBondsButton = nil;
static NSButton *gForgetButton = nil;

static NSTextField *gHotkeyField = nil;
static NSTextField *gRateField = nil;
static NSButton *gKeyboardCheck = nil;
static NSSegmentedControl *gModeControl = nil;
static NSComboBox *gResolutionCombo = nil;
static EHBArrangeView *gArrangeView = nil;
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

- (void)updateClicked:(id)sender {
  (void)sender;
  goGuiUpdateClicked();
}

- (void)checkUpdatesClicked:(id)sender {
  (void)sender;
  goGuiCheckUpdatesClicked();
}

- (void)toggleAutoUpdatesClicked:(id)sender {
  (void)sender;
  goGuiToggleAutoUpdatesClicked();
}

- (void)screensChanged:(NSNotification *)note {
  (void)note;
  goGuiDisplaysChanged();
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
  NSMenuItem *check = [[NSMenuItem alloc] initWithTitle:@"Check for Updates…"
                                                 action:@selector(checkUpdatesClicked:)
                                          keyEquivalent:@""];
  [check setTarget:gController];
  [appMenu addItem:check];
  gAutoUpdateItem = [[NSMenuItem alloc]
      initWithTitle:@"Check for Updates Automatically"
             action:@selector(toggleAutoUpdatesClicked:)
      keyEquivalent:@""];
  [gAutoUpdateItem setTarget:gController];
  [appMenu addItem:gAutoUpdateItem];
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
  NSBox *statusBox = makeBox(root, @"Connection & Status", 460, kStatusBoxHeight);
  gStatusBox = statusBox;
  NSView *sv = [statusBox contentView];
  CGFloat sw = NSWidth([sv bounds]);

  gBanner = makeLabel(sv, @"", 0, 241, sw, YES);
  [gBanner setTextColor:[NSColor systemRedColor]];
  [gBanner setHidden:YES];

  gGrantButton = makeButton(sv, @"Grant Permission…", 0, 211, 170,
                            @selector(grantClicked:));
  gSettingsButton = makeButton(sv, @"Open System Settings", 180, 211, 190,
                               @selector(settingsClicked:));
  [gGrantButton setHidden:YES];
  [gSettingsButton setHidden:YES];
  // Shares the row with the permission buttons; only one set shows at once.
  gUpdateButton = makeButton(sv, @"Install and relaunch", 0, 211, 170,
                             @selector(updateClicked:));
  [gUpdateButton setHidden:YES];

  const CGFloat labelWidth = 90;
  const CGFloat valueX = 100;
  makeLabel(sv, @"Bridge:", 0, 176, labelWidth, NO);
  gStatusBridge = makeValue(sv, @"Stopped", valueX, 176, sw - valueX);
  makeLabel(sv, @"Device:", 0, 149, labelWidth, NO);
  gStatusDevice = makeValue(sv, @"-", valueX, 149, sw - valueX);
  makeLabel(sv, @"Firmware:", 0, 122, labelWidth, NO);
  gStatusFirmware = makeValue(sv, @"-", valueX, 122, sw - valueX);
  makeLabel(sv, @"Bluetooth:", 0, 95, labelWidth, NO);
  gStatusBluetooth = makeValue(sv, @"-", valueX, 95, sw - valueX);

  // Two rows of buttons: the bridge on the upper one, with the update check
  // beside it where it can be found; device maintenance on the lower.
  gStartButton = makeButton(sv, @"Start", 0, 52, 100, @selector(startClicked:));
  gStopButton = makeButton(sv, @"Stop", 110, 52, 100, @selector(stopClicked:));
  makeButton(sv, @"Check for Updates…", sw - 180, 52, 180,
             @selector(checkUpdatesClicked:));
  gBondsButton = makeButton(sv, @"Clear device bonds", sw - 170, 16, 170,
                            @selector(bondsClicked:));
  gForgetButton = makeButton(sv, @"Forget device", sw - 330, 16, 150,
                             @selector(forgetClicked:));
  [gStopButton setEnabled:NO];

  // --- Input Settings -----------------------------------------------------
  NSBox *inputBox = makeBox(root, @"Input Settings", 320, 130);
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
  NSBox *layoutBox = makeBox(root, @"Device Layout", 20, 280);
  NSView *lv = [layoutBox contentView];
  CGFloat lw = NSWidth([lv bounds]);

  // Row 1: find the device by name. Matches drop down under the field as
  // the user types, and the best one fills the resolution straight away.
  makeLabel(lv, @"Device:", 0, 212, 130, NO);
  gDeviceSearch = [[NSSearchField alloc]
      initWithFrame:NSMakeRect(135, 210, lw - 135, 24)];
  [gDeviceSearch setPlaceholderString:@"Phone or tablet name"];
  [gDeviceSearch setDelegate:gController];
  [gDeviceSearch setTarget:gController];
  [gDeviceSearch setAction:@selector(deviceSearchAction:)];
  [lv addSubview:gDeviceSearch];

  // Row 2: the resolution itself — what is actually saved — and its
  // orientation, which is just the same two numbers the other way round.
  makeLabel(lv, @"Device resolution:", 0, 178, 130, NO);
  gResolutionCombo = [[NSComboBox alloc]
      initWithFrame:NSMakeRect(135, 176, 150, 24)];
  [gResolutionCombo setEditable:YES];
  [gResolutionCombo setDelegate:gController];
  [lv addSubview:gResolutionCombo];

  makeLabel(lv, @"Orientation:", lw - 250, 178, 110, NO);
  gOrientationPopup = [[NSPopUpButton alloc]
      initWithFrame:NSMakeRect(lw - 135, 176, 135, 25)];
  [gOrientationPopup setTarget:gController];
  [gOrientationPopup setAction:@selector(orientationChanged:)];
  [lv addSubview:gOrientationPopup];

  // The arrangement: the desktop's displays as macOS has them arranged,
  // and the device beside them on whichever side it sits. Dragging the
  // device to another side is how the side is chosen.
  gArrangeView = [[EHBArrangeView alloc]
      initWithFrame:NSMakeRect(0, 36, lw, kArrangeHeight)];
  [gArrangeView setToolTip:@"Drag the device to the side of your displays it sits on."];
  [lv addSubview:gArrangeView];

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

  [[NSNotificationCenter defaultCenter]
      addObserver:gController
         selector:@selector(screensChanged:)
             name:NSApplicationDidChangeScreenParametersNotification
           object:nil];

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
                   int autoSwitch, const char *resolution) {
  [gHotkeyField setStringValue:[NSString stringWithUTF8String:hotkey]];
  [gRateField setStringValue:[NSString stringWithFormat:@"%d", rateHz]];
  [gKeyboardCheck setState:captureKeyboard ? NSControlStateValueOn
                                           : NSControlStateValueOff];
  [gModeControl setSelectedSegment:autoSwitch ? 0 : 1];
  [gResolutionCombo setStringValue:[NSString stringWithUTF8String:resolution]];
}

int ehbGuiDisplays(EhbDisplay *out, int max) {
  CGDirectDisplayID ids[32];
  uint32_t count = 0;
  if (CGGetActiveDisplayList(32, ids, &count) != kCGErrorSuccess) {
    return 0;
  }
  CGDirectDisplayID main = CGMainDisplayID();
  int written = 0;
  for (uint32_t i = 0; i < count && written < max; i++) {
    CGRect bounds = CGDisplayBounds(ids[i]);
    if (bounds.size.width <= 0 || bounds.size.height <= 0) {
      continue;
    }
    EhbDisplay *d = &out[written++];
    memset(d, 0, sizeof(*d));
    d->x = bounds.origin.x;
    d->y = bounds.origin.y;
    d->w = bounds.size.width;
    d->h = bounds.size.height;
    // From the EDID; 0 for displays that do not report one.
    CGSize mm = CGDisplayScreenSize(ids[i]);
    d->widthMM = mm.width;
    d->heightMM = mm.height;
    d->primary = (ids[i] == main) ? 1 : 0;
    // The marketing name lives on NSScreen, matched by display id.
    NSString *name = nil;
    for (NSScreen *screen in [NSScreen screens]) {
      NSNumber *number = [[screen deviceDescription] objectForKey:@"NSScreenNumber"];
      if (number && [number unsignedIntValue] == ids[i]) {
        name = [screen localizedName];
        break;
      }
    }
    if ([name length] == 0) {
      name = [NSString stringWithFormat:@"Display %d", written];
    }
    strncpy(d->name, [name UTF8String], sizeof(d->name) - 1);
  }
  return written;
}

void ehbGuiArrangeSize(double *width, double *height) {
  NSRect bounds = [gArrangeView bounds];
  *width = NSWidth(bounds);
  *height = NSHeight(bounds);
}

void ehbGuiArrangeBegin(int dragging) {
  [gArrangeView.displayRects removeAllObjects];
  [gArrangeView.displayNames removeAllObjects];
  [gArrangeView.displayPrimary removeAllObjects];
  gArrangeView.dragging = dragging ? YES : NO;
}

void ehbGuiArrangeAddDisplay(EhbRect rect, const char *name, int primary) {
  [gArrangeView.displayRects addObject:[NSValue valueWithRect:NSMakeRect(rect.x, rect.y, rect.w, rect.h)]];
  [gArrangeView.displayNames addObject:[NSString stringWithUTF8String:name]];
  [gArrangeView.displayPrimary addObject:@(primary ? YES : NO)];
}

void ehbGuiArrangeSetDevice(EhbRect rect, const char *label) {
  gArrangeView.deviceRect = NSMakeRect(rect.x, rect.y, rect.w, rect.h);
  gArrangeView.deviceLabel = [NSString stringWithUTF8String:label];
}

void ehbGuiArrangeEnd(void) {
  [gArrangeView setNeedsDisplay:YES];
  [[gArrangeView window] invalidateCursorRectsForView:gArrangeView];
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
  gArrangeView.enabled = running ? NO : YES;
  [gArrangeView setNeedsDisplay:YES];
  [[gArrangeView window] invalidateCursorRectsForView:gArrangeView];
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

// Grows or shrinks the status box and the window by the banner strip. The
// window keeps its top edge, so from the user's side the bottom simply moves
// up when a permission lands. Animated only once the window is on screen;
// the initial call happens before it is shown.
static void layoutBannerStrip(BOOL visible) {
  CGFloat boxHeight = visible ? kStatusBoxHeight : kStatusBoxHeight - kBannerStripHeight;
  if (NSHeight([gStatusBox frame]) == boxHeight) {
    return;
  }
  NSRect box = [gStatusBox frame];
  box.size.height = boxHeight;
  [gStatusBox setFrame:box];

  CGFloat windowHeight = visible ? kWindowHeight : kWindowHeight - kBannerStripHeight;
  NSRect frame = [gWindow frame];
  CGFloat delta = windowHeight - NSHeight([gWindow contentRectForFrameRect:frame]);
  frame.origin.y -= delta;
  frame.size.height += delta;
  [gWindow setFrame:frame display:YES animate:[gWindow isVisible]];
}

void ehbGuiSetBanner(const char *message, int visible, int buttons, int isError) {
  if (message) {
    [gBanner setStringValue:[NSString stringWithUTF8String:message]];
  }
  [gBanner setTextColor:isError ? [NSColor systemRedColor] : [NSColor labelColor]];
  [gBanner setHidden:visible ? NO : YES];
  BOOL permission = visible && buttons == EHB_BANNER_PERMISSION;
  BOOL update = visible && buttons == EHB_BANNER_UPDATE;
  [gGrantButton setHidden:permission ? NO : YES];
  [gSettingsButton setHidden:permission ? NO : YES];
  [gUpdateButton setHidden:update ? NO : YES];
  layoutBannerStrip(visible ? YES : NO);
}

void ehbGuiSetAutoUpdateChecked(int checked) {
  [gAutoUpdateItem setState:checked ? NSControlStateValueOn : NSControlStateValueOff];
}

void ehbGuiShowAlert(const char *title, const char *message, int isError) {
  NSAlert *alert = [[NSAlert alloc] init];
  [alert setMessageText:[NSString stringWithUTF8String:title]];
  [alert setInformativeText:[NSString stringWithUTF8String:message]];
  [alert setAlertStyle:isError ? NSAlertStyleCritical : NSAlertStyleInformational];
  [alert addButtonWithTitle:@"OK"];
  [alert runModal];
}

int ehbGuiAskUpdate(const char *title, const char *message, const char *notes) {
  NSAlert *alert = [[NSAlert alloc] init];
  [alert setMessageText:[NSString stringWithUTF8String:title]];
  [alert setInformativeText:[NSString stringWithUTF8String:message]];
  [alert setAlertStyle:NSAlertStyleInformational];
  [alert addButtonWithTitle:@"Install and Relaunch"];
  [alert addButtonWithTitle:@"Later"];

  // The release notes, read-only in a scrolling box so a long entry does not
  // turn the alert into a tower.
  NSString *text = [NSString stringWithUTF8String:notes];
  if ([text length] > 0) {
    NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(0, 0, 440, 180)];
    [scroll setHasVerticalScroller:YES];
    [scroll setBorderType:NSBezelBorder];
    NSTextView *view = [[NSTextView alloc]
        initWithFrame:NSMakeRect(0, 0, [scroll contentSize].width, [scroll contentSize].height)];
    [view setEditable:NO];
    [view setSelectable:YES];
    [view setFont:[NSFont systemFontOfSize:12]];
    [view setTextContainerInset:NSMakeSize(6, 6)];
    [view setString:text];
    [view setVerticallyResizable:YES];
    [view setHorizontallyResizable:NO];
    [view setAutoresizingMask:NSViewWidthSizable];
    [[view textContainer] setWidthTracksTextView:YES];
    [scroll setDocumentView:view];
    [alert setAccessoryView:scroll];
  }
  return [alert runModal] == NSAlertFirstButtonReturn ? 1 : 0;
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

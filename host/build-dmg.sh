#!/usr/bin/env bash
# Packages dist/ESP HID Bridge.app into a drag-to-install .dmg.
#
# Usage: ./build-dmg.sh [version]        (run build-macos.sh first)
#
# Produces dist/ESP-HID-Bridge-<version>.dmg: a window containing the app and
# an Applications shortcut side by side, so the user drags one onto the other.
#
# NOTE: a .dmg does not change Gatekeeper's mind. The app inside is ad-hoc
# signed rather than notarized, so the first launch still needs the quarantine
# workaround documented in README.md. Only a paid Developer ID plus
# notarization removes that step.

set -euo pipefail

VERSION="${1:-dev}"
APP_NAME="ESP HID Bridge"
VOLUME_NAME="ESP HID Bridge"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

DIST_DIR="dist"
APP_PATH="$DIST_DIR/$APP_NAME.app"
STAGING="build/dmg-staging"
RW_DMG="build/rw.dmg"
FINAL_DMG="$DIST_DIR/ESP-HID-Bridge-${VERSION}.dmg"
BACKGROUND="packaging/macos/dmg-background.tiff"

# Window geometry. These must match the artwork in
# packaging/macos/make-dmg-background.c.
WINDOW_WIDTH=600
WINDOW_HEIGHT=400
ICON_SIZE=96
ICON_Y=190
APP_ICON_X=150
APPLICATIONS_ICON_X=450

if [ ! -d "$APP_PATH" ]; then
  echo "error: $APP_PATH not found — run ./build-macos.sh first" >&2
  exit 1
fi

echo "Packaging $APP_NAME $VERSION as a disk image"

# hdiutil refuses to attach a second image with the same volume name, and a
# stale mount from an interrupted run is easy to leave behind.
if [ -d "/Volumes/$VOLUME_NAME" ]; then
  echo "  detaching a stale /Volumes/$VOLUME_NAME"
  hdiutil detach "/Volumes/$VOLUME_NAME" -force >/dev/null 2>&1 || true
fi

rm -rf "$STAGING" "$RW_DMG" "$FINAL_DMG"
mkdir -p "$STAGING/.background" "$DIST_DIR"

echo "  staging contents"
cp -R "$APP_PATH" "$STAGING/"
ln -s /Applications "$STAGING/Applications"
cp "$BACKGROUND" "$STAGING/.background/background.tiff"

# Size the read-write image from the payload plus generous slack; a too-small
# image fails to attach in ways that are tedious to diagnose.
payload_kb=$(du -sk "$STAGING" | awk '{print $1}')
image_mb=$(((payload_kb / 1024) + 40))

echo "  creating read-write image (${image_mb}MB)"
hdiutil create -srcfolder "$STAGING" -volname "$VOLUME_NAME" -fs HFS+ \
  -format UDRW -size "${image_mb}m" "$RW_DMG" >/dev/null

echo "  attaching"
device=$(hdiutil attach -readwrite -noverify -noautoopen "$RW_DMG" |
  awk '/^\/dev\// {print $1; exit}')
mount_point="/Volumes/$VOLUME_NAME"

# Spotlight starts indexing a freshly mounted volume, and an indexer holding
# files open is one of the two things that make the detach below fail with
# "Resource busy". Turn it off for this volume; it is thrown away anyway.
mdutil -i off "$mount_point" >/dev/null 2>&1 || true

# Give the Finder a moment to notice the new volume before scripting it.
sleep 2

# Styling the window is the one step that needs the Finder, which is not
# reliably scriptable on a headless CI runner. Treat failure as cosmetic:
# without it the DMG still opens showing the app and the Applications
# shortcut, which is the part that matters.
echo "  applying window layout"
if osascript >/dev/null 2>&1 <<EOF
tell application "Finder"
  tell disk "$VOLUME_NAME"
    open
    set current view of container window to icon view
    set toolbar visible of container window to false
    set statusbar visible of container window to false
    set the bounds of container window to {200, 150, $((200 + WINDOW_WIDTH)), $((150 + WINDOW_HEIGHT))}
    set viewOptions to the icon view options of container window
    set arrangement of viewOptions to not arranged
    set icon size of viewOptions to $ICON_SIZE
    set background picture of viewOptions to file ".background:background.tiff"
    set position of item "$APP_NAME.app" of container window to {$APP_ICON_X, $ICON_Y}
    set position of item "Applications" of container window to {$APPLICATIONS_ICON_X, $ICON_Y}
    close
    open
    update without registering applications
    delay 1
    close
  end tell
end tell
EOF
then
  echo "    layout applied"
else
  echo "    warning: Finder styling unavailable (headless session?);" >&2
  echo "    the image is still a working drag-to-Applications installer" >&2
fi

# Make the staged layout the volume's default view for everyone who opens it.
sync

echo "  detaching"
# The other thing that holds the volume is the Finder itself, for a few
# seconds after it has been scripted; the v2.4.0 release lost its macOS build
# to exactly this, both a plain and a forced detach failing at once. Close
# the window, then keep trying for a while before resorting to force.
osascript -e "tell application \"Finder\" to close (every window whose name is \"$VOLUME_NAME\")" \
  >/dev/null 2>&1 || true
detached=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if hdiutil detach "$device" >/dev/null 2>&1; then
    detached=1
    break
  fi
  sleep 2
done
if [ "$detached" -eq 0 ]; then
  echo "    volume still busy after 20 s; forcing" >&2
  hdiutil detach "$device" -force >/dev/null || {
    sleep 5
    hdiutil detach "$device" -force >/dev/null
  }
fi

echo "  converting to compressed read-only image"
hdiutil convert "$RW_DMG" -format UDZO -imagekey zlib-level=9 -o "$FINAL_DMG" >/dev/null
rm -f "$RW_DMG"
rm -rf "$STAGING"

# Same identity as the app when one is available; the image's own signature
# has no bearing on the grants, this just keeps the two consistent.
SIGNING_IDENTITY="${SIGNING_IDENTITY:-}"
if [ -z "$SIGNING_IDENTITY" ]; then
  SIGNING_IDENTITY="$(security find-identity -p codesigning 2>/dev/null |
    sed -n 's/.*"\(ESP HID Bridge[^"]*\)".*/\1/p' | head -n1 || true)"
fi
codesign --force --sign "${SIGNING_IDENTITY:--}" --timestamp=none "$FINAL_DMG"
codesign --verify --strict "$FINAL_DMG"

echo
echo "Built: $SCRIPT_DIR/$FINAL_DMG"
echo "  $(du -h "$FINAL_DMG" | awk '{print $1}')"
echo
if [ -n "$SIGNING_IDENTITY" ]; then
  echo "Reminder: the app inside is signed as \"$SIGNING_IDENTITY\" but not"
else
  echo "Reminder: the app inside is ad-hoc signed, not"
fi
echo "notarized. On first launch macOS will refuse to open it until the user runs:"
echo "  xattr -dr com.apple.quarantine \"/Applications/$APP_NAME.app\""
echo "or approves it under System Settings > Privacy & Security > Open Anyway."

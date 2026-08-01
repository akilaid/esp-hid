#!/usr/bin/env bash
# Builds "ESP HID Bridge.app" as a universal (arm64 + x86_64) macOS bundle.
#
# Usage: ./build-macos.sh [version]
#   version defaults to "dev"; CI passes the release tag, e.g. v2.0.2
#
# Prerequisites: Go 1.22+ and the Xcode Command Line Tools (cgo needs clang
# and the macOS SDK). Output lands in dist/.
#
# The bundle is ad-hoc signed. That is enough for macOS to run it locally,
# but it is not notarized, so a copy downloaded through a browser carries the
# quarantine attribute and Gatekeeper will refuse it until the user either
# right-clicks -> Open once, or runs:
#   xattr -dr com.apple.quarantine "/Applications/ESP HID Bridge.app"

set -euo pipefail

VERSION="${1:-dev}"
APP_NAME="ESP HID Bridge"
BUNDLE_ID="com.espbridge.hid-bridge"
EXECUTABLE="esp-hid-bridge"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

BUILD_DIR="build/macos"
DIST_DIR="dist"
APP_DIR="$DIST_DIR/$APP_NAME.app"

# CFBundleShortVersionString must be a dotted number; a tag like "v2.0.2" or a
# placeholder like "dev" is not, so normalize it.
SHORT_VERSION="${VERSION#v}"
if ! [[ "$SHORT_VERSION" =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
  SHORT_VERSION="0.0.0"
fi

# Pin the toolchain to Xcode's clang. A Homebrew clang first on PATH does not
# understand -arch for the macOS SDK and breaks the cross-architecture slice
# with confusing errors.
CC="$(xcrun --find clang)"
export CC
SDKROOT="$(xcrun --sdk macosx --show-sdk-path)"
export SDKROOT
export MACOSX_DEPLOYMENT_TARGET=11.0

echo "Building $APP_NAME $VERSION (bundle version $SHORT_VERSION)"

rm -rf "$BUILD_DIR" "$APP_DIR"
mkdir -p "$BUILD_DIR"

for arch in arm64 amd64; do
  echo "  compiling darwin/$arch"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" \
    go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o "$BUILD_DIR/$EXECUTABLE-$arch" ./cmd/bridge
done

lipo -create -output "$BUILD_DIR/$EXECUTABLE" \
  "$BUILD_DIR/$EXECUTABLE-arm64" "$BUILD_DIR/$EXECUTABLE-amd64"

# Assert both slices really made it in; a silently single-architecture binary
# would only be discovered by an Intel user.
if ! lipo -info "$BUILD_DIR/$EXECUTABLE" | grep -q 'x86_64' ||
  ! lipo -info "$BUILD_DIR/$EXECUTABLE" | grep -q 'arm64'; then
  echo "error: universal binary is missing an architecture:" >&2
  lipo -info "$BUILD_DIR/$EXECUTABLE" >&2
  exit 1
fi
lipo -info "$BUILD_DIR/$EXECUTABLE"

echo "  assembling bundle"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
cp "$BUILD_DIR/$EXECUTABLE" "$APP_DIR/Contents/MacOS/$EXECUTABLE"

sed -e "s|@SHORT_VERSION@|$SHORT_VERSION|g" \
  -e "s|@BUNDLE_ID@|$BUNDLE_ID|g" \
  packaging/macos/Info.plist.in >"$APP_DIR/Contents/Info.plist"

# Icons are generated from the existing Windows .ico files, which carry a
# 512x512 variant, so macOS needs no separate art.
echo "  generating icons"
ICONSET="$BUILD_DIR/AppIcon.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
  sips -s format png -Z "$size" app.ico --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  sips -s format png -Z "$((size * 2))" app.ico --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP_DIR/Contents/Resources/AppIcon.icns"

# Menu-bar images: one for idle, one shown while remote mode is active.
sips -s format png -Z 18 app.ico --out "$APP_DIR/Contents/Resources/status-idle.png" >/dev/null
sips -s format png -Z 36 app.ico --out "$APP_DIR/Contents/Resources/status-idle@2x.png" >/dev/null
sips -s format png -Z 18 on.ico --out "$APP_DIR/Contents/Resources/status-active.png" >/dev/null
sips -s format png -Z 36 on.ico --out "$APP_DIR/Contents/Resources/status-active@2x.png" >/dev/null

echo "  signing (ad-hoc)"
# Deliberately no --options runtime: the hardened runtime buys nothing
# without notarization and only adds Gatekeeper friction.
codesign --force --sign - "$APP_DIR/Contents/MacOS/$EXECUTABLE"
codesign --force --sign - "$APP_DIR"
codesign --verify --strict --verbose=2 "$APP_DIR"

echo
echo "Built: $SCRIPT_DIR/$APP_DIR"
echo "Run it with: open \"$APP_DIR\""
echo
echo "On first launch macOS will ask for Accessibility and Input Monitoring."
echo "Because the signature is ad-hoc it changes on every rebuild, so those"
echo "grants must be renewed after each new build. To reset a confused state:"
echo "  tccutil reset Accessibility $BUNDLE_ID"
echo "  tccutil reset ListenEvent $BUNDLE_ID"

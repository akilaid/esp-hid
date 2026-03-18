#!/usr/bin/env bash
# Build script for the macOS ESP HID Bridge.
# Usage: ./build-production.sh
#
# Prerequisites:
#   brew install go   (Go 1.22+)
#   xcode-select --install   (Command Line Tools for CGo)
#   Run once: go mod tidy && go mod download

set -euo pipefail

BINARY="esp-hid-bridge"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "Building $BINARY for macOS ($(uname -m))..."

go build \
  -trimpath \
  -ldflags="-s -w" \
  -o "$BINARY" \
  .

echo "Built: $SCRIPT_DIR/$BINARY"

# Optionally package as a .app bundle
if command -v fyne &>/dev/null; then
  echo "Packaging as .app bundle with 'fyne package'..."
  fyne package -os darwin -name "ESP HID Bridge" -appID com.esp-hid-bridge.macos
  echo "Done: ESP HID Bridge.app"
else
  echo ""
  echo "Tip: install the fyne CLI tool to create a .app bundle:"
  echo "  go install fyne.io/fyne/v2/cmd/fyne@latest"
  echo "  ./build-production.sh"
fi

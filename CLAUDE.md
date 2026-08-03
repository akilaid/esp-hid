# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A bridge that forwards a PC's mouse/keyboard to an ESP32 over USB; the ESP32
re-emits the input as Bluetooth LE HID (combined mouse + keyboard) to a paired
phone/tablet. The PC is the "host", the BLE target is the "slave".

**The repo holds two generations. v2 is the live one; work there unless
explicitly asked otherwise.**

| Path | Generation | Role |
|---|---|---|
| `firmware-idf/` | **v2** | ESP-IDF v6.0, ESP32-C3, NimBLE via `esp_hid`. |
| `host/` | **v2** | Go sender, Windows + macOS, single module `esp-hid/host`. |
| `firmware/` | v1 | Arduino sketch, `ESP32-BLE-Combo` on NimBLE. Superseded. |
| `software/` | v1 | Go Windows sender, module `esp-hid/software`. Superseded. |

The two generations speak different wire protocols and cannot interoperate.
The v1 tree is kept only for boards still running the Arduino sketch; do not
port v2 changes into it.

## The contract

v2 is held together by the **binary wire protocol** in
`firmware-idf/docs/PROTOCOL.md`: framed as
`0xAA 0x55 | type | len | payload | crc8`, CRC-8 poly `0x07` over
`type|len|payload`, little-endian payloads, max 32-byte payload.

It has **three hand-maintained implementations**, and a change to any message
means changing all of them plus the doc:

- `firmware-idf/main/protocol.c` (+ `protocol.h`)
- `host/internal/protocol/protocol.go`
- `firmware-idf/tools/hidctl.py`

They are kept honest by `firmware-idf/tools/test_protocol.c` and
`host/internal/protocol/protocol_test.go`, both asserting the test vectors in
PROTOCOL.md.

Transport facts that shape the design: the C3's native USB Serial/JTAG port
(VID/PID `303A:1001`) ignores baud, and **opening the port does not reset the
chip**, so device state survives host reconnects — the host must send
`RELEASE_ALL` + `GET_STATUS` on connect. Firmware logs go to UART0 unless
`CONFIG_BRIDGE_LOG_FRAMES=y` mirrors them into protocol `LOG` frames.

## Build / flash / run

### Firmware (from `firmware-idf/`)
```bash
get_idf                     # or: . $IDF_PATH/export.sh
idf.py set-target esp32c3   # first time only
idf.py build
idf.py -p /dev/cu.usbmodem* flash
```
Test without the GUI app: `tools/hidctl.py status` (needs a venv at
`tools/.venv` with pyserial).

### Host (from `host/`)
```bash
# Windows
./build-production.ps1              # GUI exe with embedded icons

# macOS (needs Xcode Command Line Tools; capture and GUI are cgo)
./build-macos.sh                    # universal .app in dist/

go vet ./... && go test ./...
GOOS=windows GOARCH=amd64 go build ./...   # cross-compile check
```
There is no darwin cross-compile check — the macOS layers need the macOS SDK,
so the `build-macos` CI job is what type-checks them.

`.github/workflows/release-v2.yml` (workflow_dispatch) computes the next tag
in a `meta` job, then builds Windows, macOS, and firmware in parallel. It is
the only release pipeline: the legacy `release-main.yml` was deleted because
it built the v1 app from the *same* tag namespace, so running it would have
published a v1 binary under the next `v2.x` tag. The v1 source under
`firmware/` and `software/` is still there and still buildable by hand.

## Host architecture

Pipeline, per run:

1. **`internal/capture`** — the OS input hook (Windows low-level hooks;
   macOS CGEventTap) runs on a locked OS thread and hosts the entire
   **remote-mode state machine**. While remote mode is active the callback
   *consumes* real input so it never reaches the host OS. It emits semantic
   `capture.Event`s on a channel.
2. **`internal/bridge`** — the pump. Mouse moves go into a
   `core.MovementAccumulator`; a ticker at `MoveRateHz` drains them, applies
   `core.MovementShaper` (deadzone + micro-smoothing) and `core.Backpressure`
   (drops MOVEs when the queue is congested), and enqueues encoded frames.
   `core.KeyTracker` de-dupes auto-repeat.
3. **`internal/device`** — finds the C3 by USB VID/PID, owns the serial
   session, auto-reconnect, and a 1 Hz PING / 3-missed-PONG liveness check.
   Its queue is lossy by design: `EnqueueMove` drops when full, `Enqueue`
   evicts to make room (clicks and key releases must not be lost).

### The seam
`capture.Run(ctx, Options, chan<- Event, activationAllowedFn) error` is the
**only** thing a platform must implement. Adding a platform means adding one
file behind that signature; `internal/bridge` is portable and gated
`//go:build windows || darwin` solely because it imports `capture`.

### Remote-mode state machine
Shared across platforms in `internal/capture/geometry.go` (untagged, tested):
the outer-edge activation probe, the entry inset, the dead-reckoned
`virtualCursor` with its return-pressure model, and the left-swipe tracker.
Only the OS plumbing differs per platform. The tuning constants there are
hard-won — do not adjust them casually.

**Critical invariant:** if the link drops (`activationAllowedFn` goes false),
the callback force-exits remote mode and restores the cursor. The hotkey is
intentionally inert while the link is down, so you cannot get trapped
controlling an unreachable device.

### macOS specifics
Four things the implementation must keep doing, the first three each fixing a
defect in the retired v1 macOS app:

- **Re-enable the tap** on `kCGEventTapDisabledByTimeout` /
  `ByUserInput`, plus a 1 Hz watchdog. macOS disables a tap whose callback is
  slow; without this, capture dies silently and permanently under load.
- **Never warp per motion event.** Warping suppresses local mouse events for
  ~0.25 s. Instead: dissociate with `CGAssociateMouseAndMouseCursorPosition`
  and read `kCGMouseEventDeltaX/Y`. There is no warp on *entry* either — from a
  screen edge its delta points back at the edge just crossed and trips the
  return-pressure model. Exit warps once, to the return point.
- **Forward modifiers from `flagsChanged`.** macOS never sends key down/up for
  pure modifiers. The handler reconciles all 8 usages (`0xE0..0xE7`) against
  the event flags, seeded silently on entry so the toggle combo itself is not
  forwarded. Caps Lock and Fn are deliberately excluded.
- **Hold the foreground while remote mode is engaged.** Dissociation and
  `CGDisplayHideCursor` are honoured only for the *frontmost application*
  (`CGRemoteOperation.h`: "while an application is in the foreground"), while
  the session tap keeps capturing regardless. Without the grab, minimizing the
  window — or merely clicking another app — drives the device *and* moves the
  local pointer at the same time. `capture_darwin_focus.m` grabs on entry and
  hands focus back on exit; it is the only AppKit in `internal/capture`, and it
  is deliberately async on a serial queue with a generation counter, because
  activation is slow, must not block the tap callback, and must not land after
  a fast toggle-off.

Never block in the tap callback — only the non-blocking `publish` is allowed.
`-debug-stall-capture` deliberately stalls it to exercise the recovery path.

macOS also gates capture behind two separate TCC permissions (Accessibility
*and* Input Monitoring), keyed on **code signature, not path** — so an ad-hoc
signed build loses its grant on every rebuild.

### Config
`internal/config` resolves defaults → `settings-v2.json` → CLI flags. Every
persisted field is a pointer so a missing key keeps its default. Adding a
tunable means touching `config.go` (struct + flag + validation),
`settings.go`, and `internal/ui/form.go`.

## Conventions

- **Build tags are load-bearing.** `_windows.go` / `_darwin.go` suffixes carry
  implicit GOOS constraints, and that applies to `.c`/`.h`/`.m` files too.
- **`*_cgkeycode.go` is deliberately not `_darwin.go`.** Those files are pure
  macOS lookup tables with no cgo, so keeping them untagged means they and
  their tests compile and run on every platform's CI. Do not rename them.
- Shared logic goes in an untagged file (`capture.go`, `geometry.go`,
  `form.go`); only genuine OS plumbing gets a tag.
- macOS C lives in real `.c`/`.h`/`.m` files, not cgo preamble comments.
- Firmware: everything in `namespace bridge` equivalents; per-command logic
  stays as small helpers in `main.c`'s `dispatch()`.

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
idf.py -p /dev/cu.usbmodemXXXX flash   # name the port; see below
```
Test without the GUI app: `tools/hidctl.py status` (needs a venv at
`tools/.venv` with pyserial).

Three traps here, each of which has already cost real debugging time:

- **`sdkconfig.defaults` does not apply to an existing `sdkconfig`.** It seeds
  that file once. Editing defaults on a tree that has already been configured
  changes nothing — edit `sdkconfig` too (it is gitignored), or delete it and
  reconfigure. A config change that appears to have no effect is this.
- **Always pass an explicit `-p`.** Auto-detect picks whichever Espressif board
  answers first, and every native-USB Espressif chip shares `303A:1001` — the
  same ambiguity `internal/device` exists to solve. On a machine with several
  boards it will flash the wrong one; only esptool's chip-ID check catches it,
  and only when the chips differ. Do not rely on a `/dev/cu.usbmodem*` glob.
- **Panics are invisible over USB.** The console is UART0 (GPIO20/21) and the
  secondary USB console is disabled so nothing but protocol frames touch the
  CDC port, so a crash looks like a silent boot loop. To read a panic,
  temporarily set `CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG=y` in
  `sdkconfig`, reflash, and read the port as plain text — the frame decoder
  resyncs around the interleaved console output. Revert it afterwards.

### Host (from `host/`)
```bash
# Windows
./build-production.ps1              # GUI exe with embedded icons

# macOS (needs Xcode Command Line Tools; capture and GUI are cgo)
./build-macos.sh                    # universal .app in dist/

go vet ./... && go test ./...
GOOS=windows GOARCH=amd64 go build ./...   # cross-compile check
GOOS=linux GOARCH=amd64 go build ./...     # reproduces the ubuntu CI job
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
3. **`internal/device`** — identifies the board (see below), owns the serial
   session, auto-reconnect, and a 1 Hz PING / 3-missed-PONG liveness check.
   Its queue is lossy by design: `EnqueueMove` drops when full, `Enqueue`
   evicts to make room (clicks and key releases must not be lost).

### Device identification
`303A:1001` is shared by **every** Espressif chip with native USB, so it
narrows the field but never names a board. Picking the first match is a silent
misroute: the wrong ESP32 opens fine and answers `HELLO`, because `HELLO`
carries only compile-time constants.

The identity is the **USB serial number**, which an Espressif ROM descriptor
reports as the board's factory MAC. `internal/device/discover.go` holds the whole of
it: `normalizeSerial`, the pure `selectPort`, and `probe`.

- Unbound, the app probes each candidate with **one** `GET_STATUS` (5 bytes,
  `AA 55 02 00 2A`, no `0x0A`/`0x0D` so a foreign REPL cannot execute it),
  learns the responder's serial and persists it. Bound, it never probes again.
- **Never substitute.** If the bound board is absent the app waits, even when
  exactly one other board is attached. `TestBoundAbsentNeverProbes` pins this.
- The probe cache is the safety property: `Run` retries every 750 ms, so
  without it the user's other boards would be written to several times a
  second. Cache a board once it has been *written to*; never cache a refused
  open (nothing was written, and the busy port may be the bridge itself).
- **`openWithTimeout` is not defensive padding.** A wedged CDC endpoint hangs
  `open()` forever — measured, and `O_NONBLOCK` does not help — which would
  hang the reconnect loop and the whole app. Such a board is cached and skipped.
- Do **not** pass `serial.Mode.InitialStatusBits`. It looks like cheap
  hardening; it makes the library issue a `TIOCMSET` ioctl that never returns
  on an Espressif USB CDC endpoint. Nil skips the ioctl, which is what `session()`
  has always done.
- Persist via `config.SaveDeviceSerial`, never `config.Save(cfg)`: `-cli` and
  `-gui=false` force `GUIMode` false, so writing the whole config back after a
  diagnostic run would silently make the app launch headless from then on.
- `EventDeviceLearned` is emitted with `emitBlocking`. `emit` drops on a full
  channel, and losing this event un-learns the device.

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

**Edge entry is armed differently per platform, on purpose.** Windows crosses
when the cursor reaches the outer edge; macOS additionally requires
`edgeEntryPressure` (geometry.go) to accumulate outward motion, because a
single-display Mac puts the Dock, menu bar and close buttons on those same
borders. This is not an inconsistency to tidy up. It also cannot simply be
ported to Windows: the Win32 hook path reads absolute positions
(`lParam.Pt`) and has no delta once the cursor is clamped, whereas the
CGEventTap keeps reporting `kCGMouseEventDeltaX/Y` (measured: 41 of 42 frozen
events carried one).

`edgeArmed` remains load-bearing regardless. `returnPointInRect` lands the
cursor *exactly on* the activation edge for all four host sides, so
`canActivateFromHostEdge` is true the instant remote mode exits; only the
disarm stops an immediate re-entry loop. Pressure narrows that window but must
never become the only guard.

The return lands level with the recorded crossing point (`entryPoint`), not the
middle of the edge. `remoteAnchor` stays the monitor *centre* on purpose: it is
the Windows pin point and delta origin while remote, and on both platforms it
is the key that re-finds the entry monitor on exit. Feed `returnPointInRect`
the entry point, never the anchor.

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
- **Suppress the local pointer without needing the foreground.** Both
  `CGDisplayHideCursor` and `CGAssociateMouseAndMouseCursorPosition` are
  honoured only for the *frontmost application* (`CGRemoteOperation.h`: "while
  an application is in the foreground"), while the session tap captures
  regardless. So minimizing the window drove the device *and* moved the local
  pointer at once. Two mechanisms fix it, and both are needed:
  `ehbEnableBackgroundCursor` sets the private `SetsCursorInBackground`
  connection property once at startup, which is what makes the hide stick; and
  `handleMouseMove` warps the pointer back to `pinPoint` on every motion event,
  which is what stops it wandering and lighting up whatever it passes over.
  Do **not** try to fix this by activating the app instead — that was tried,
  and macOS 14+ refuses activation from a background app, even one ordering a
  window front. Measurements behind the design: warping emits no events (0
  after 2000 warps, via `CGEventSourceCounterForEventType`) and costs ~20µs,
  so it is safe inside the callback.

Never block in the tap callback — only the non-blocking `publish` is allowed.
`-debug-stall-capture` deliberately stalls it to exercise the recovery path.

macOS also gates capture behind two separate TCC permissions (Accessibility
*and* Input Monitoring), keyed on **code signature, not path** — so an ad-hoc
signed build loses its grant on every rebuild. Release builds and local
builds are therefore signed with one self-signed certificate named "ESP HID
Bridge" (`packaging/macos/make-signing-cert.sh` creates it; `build-macos.sh`
finds it by name, CI imports it from `MACOS_SIGNING_CERT_P12`). Without it
the scripts fall back to ad-hoc and say so.

### Self-update
`internal/update` checks GitHub Releases, verifies the download against the
release's `SHA256SUMS`, and swaps the program in place: on macOS the zip is
unpacked with `ditto` into a staging dir *beside* the bundle (same volume, so
the two renames are atomic), `codesign --verify` gates the swap, the old
bundle is kept as `.previous` until the next launch, and a detached shell
`open`s the app once this process has exited; on Windows the running exe is
renamed to `.old` (allowed while running; overwriting is not) and the new one
started. `ui/updates.go` is the shared state machine both GUIs drive; it
never installs without a click. Dev builds (`version` not a tag) never see
updates — `update.ErrDevBuild`.

### Config
`internal/config` resolves defaults → `settings-v2.json` → CLI flags. Every
persisted field is a pointer so a missing key keeps its default. Adding a
tunable means touching `config.go` (struct + flag + validation),
`settings.go`, and `internal/ui/form.go`.

## Conventions

- **Build tags are load-bearing.** `_windows.go` / `_darwin.go` suffixes carry
  implicit GOOS constraints, and that applies to `.c`/`.h`/`.m` files too. The
  GOOS must be the **final** underscore-separated element:
  `capture_focus_darwin.m` is constrained, `capture_darwin_focus.m` is not, and
  an unconstrained `.m` file fails the ubuntu job with "Objective-C source
  files not allowed when not using cgo" — a darwin-only build never notices.
  `GOOS=linux GOARCH=amd64 go build ./...` reproduces that job locally.
- **`*_cgkeycode.go` is deliberately not `_darwin.go`.** Those files are pure
  macOS lookup tables with no cgo, so keeping them untagged means they and
  their tests compile and run on every platform's CI. Do not rename them.
- Shared logic goes in an untagged file (`capture.go`, `geometry.go`,
  `form.go`); only genuine OS plumbing gets a tag.
- macOS C lives in real `.c`/`.h`/`.m` files, not cgo preamble comments.
- Firmware: everything in `namespace bridge` equivalents; per-command logic
  stays as small helpers in `main.c`'s `dispatch()`.

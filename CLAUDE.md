# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A three-part bridge that forwards a PC's mouse/keyboard to an ESP32 over USB serial; the ESP32 re-emits the input as Bluetooth LE HID (combined mouse + keyboard) to a paired phone/tablet. The PC acts as "host", the BLE target as the "slave".

The whole system is held together by one contract: the **serial protocol** (newline-delimited UTF-8 text commands). The Go sender produces these strings; the firmware parses and replays them as HID reports. Changing a command name, argument format, or adding a command requires edits on *both* sides (`software*/serial_writer.go` + `software*/events.go` enqueue side, and `firmware/command_dispatch.cpp` dispatch side). The canonical command list lives in README.md ("Serial Protocol") and `firmware/command_dispatch.cpp`.

## Three components

- `firmware/` — ESP32 Arduino sketch (C++). Entry point `firmware.ino`.
- `software/` — Windows sender (Go). Every `.go` file is `//go:build windows`.
- `software-macos/` — macOS sender (Go, Fyne GUI + CGo). Platform code uses the `_darwin.go` filename suffix.

The two senders are independent Go modules (`esp-hid/software`, `esp-hid/software-macos`) that re-implement the same pipeline against different OS input APIs. They share concepts and the serial protocol, **not** code — a change to capture/remote-mode logic usually needs to be mirrored in both.

## Build / flash / run

### Firmware (from `firmware/`)
```powershell
arduino-cli compile --fqbn esp32:esp32:esp32 --libraries libraries --output-dir out .
arduino-cli upload -p COM9 -b esp32:esp32:esp32 --input-dir out -t   # replace COM9
```
`ble_combo_sources.cpp` `#include`s the bundled library's `.cpp` files directly, so the sketch compiles the patched `ESP32-BLE-Combo` (in `firmware/libraries/`) without it being installed in the Arduino `libraries` folder. The README's symlink instructions are the older alternative; the in-tree include is self-contained. The library copy is patched for ESP32 Core 3.x.x — do not replace it with the upstream version.

The bundled `ESP32-BLE-Combo` runs on the **NimBLE** stack (`NimBLE-Arduino` by h2zero, v2.x, installed via Library Manager — a hard dependency). It was ported from Bluedroid because the legacy Bluedroid BLE-HID path crashes during BLE init (`Guru Meditation / Load access fault`) on the newer ESP32-C3/S3 controllers. `press()`/`release()`/`_asciimap` keep the T-vK BleKeyboard key-code convention (ASCII for printables, `0x80+` for modifiers/specials) — this is the contract `software*/keymap.go` `vkToBleKeyCode` encodes, so changing it breaks keyboard input.

### Windows sender (from `software/`)
```powershell
go mod tidy
.\build-production.ps1                 # GUI build: embeds icons, -H=windowsgui (no console)
go build -o dev.exe .                  # plain build: console subsystem, useful for logs
.\esp-hid-bridge.exe                   # GUI (default)
.\esp-hid-bridge.exe -gui=false -port auto   # CLI
```
Live reload during dev: `air -c .air.toml`.

### macOS sender (from `software-macos/`)
```bash
go mod tidy
./build-production.sh                  # builds; packages a .app if the fyne CLI is installed
```
Needs Xcode Command Line Tools (CGo, required by Fyne).

There is no test suite. `.github/workflows/release-main.yml` is `workflow_dispatch`-only: it runs `build-production.ps1`, auto-increments the latest `vMAJOR.MINOR.PATCH` tag, and publishes the Windows EXE as a GitHub release.

## Go sender architecture (data flow)

This is the part that needs multiple files to understand. Pipeline, per run:

1. **OS input hooks** (`hooks_windows.go` `runInputHooks` / `hooks_darwin.go`) run on a locked OS thread and own the low-level keyboard/mouse hook. The callback hosts the entire **remote-mode state machine** (see below) and emits semantic `inputEvent`s into a channel. When remote mode is active the callback *consumes* (swallows) the real input so it never reaches the host OS.
2. **Capture loop** (`events.go` `runCaptureLoop`) drains the event channel. Mouse moves go into a `movementAccumulator`; a ticker at `moveRateHz` drains accumulated deltas, applies `movementShaper` (deadzone + micro-smoothing) and `moveBackpressureController` (drops/throttles MOVEs when the serial queue is congested), and enqueues `MOVE`/`SCROLL`/button/key commands as strings. `keyStateTracker` de-dupes auto-repeat key events.
3. **Command queue** — a buffered `chan string` (cap 1024). `enqueueCommand` is lossy by design: when full it silently drops `MOVE` frames but force-evicts to make room for non-MOVE commands (clicks/keys must not be lost).
4. **Write loop** (`serial_writer.go` `writeLoop`) opens the serial port (auto-detect picks the highest `COMn`), writes commands + `\n`, and owns **auto-reconnect**: any write/open error closes the port, waits `reconnectDelay`, and retries. On (re)connect it sends `RELEASE\nKEYRELEASE\n` to clear stuck buttons/keys.

`bridge_runtime.go` wraps this pipeline for the GUI (Start/Stop/Wait, goroutine lifecycle). `main.go` `runCLIBridge` wires it directly for CLI mode. Both emit `bridgeEvent`s (`bridge_events.go`) that the GUI renders as connection status and that gate remote activation.

### Remote-mode state machine (in the hook callback)
Remote mode is the "is input being forwarded to the slave" toggle. It is entered/exited three ways, all handled inside the hook callback:
- **Hotkey** (`-toggle`, default `F9`, matched against live modifier state via `currentMods`).
- **Auto / edge** (`-auto-switch`): cursor pushed to the host-side screen boundary (`-host-side`) crosses into the virtual slave. `edgeArmed` debounces this.
- **Return**: a virtual slave cursor is tracked against `-slave-res`; pushing past the far edge builds `edgeReturnPressure` until it crosses a threshold and snaps back to the host. Optional left-swipe return (`-leftreturn`).

While active, the host cursor is pinned to an anchor point and the system cursor is hidden; deltas are computed against the anchor and sent as `MOVE`. **Critical invariant:** if the serial connection drops (`remoteActivationAllowed()` goes false), the callback force-exits remote mode and restores the cursor — the hotkey is intentionally inert while serial is down so you cannot get trapped controlling a disconnected device.

### Config & settings
`config.go` defines the `config` struct and all CLI flags. Precedence: built-in defaults → persisted settings (`%AppData%\ESP HID Bridge\settings.json` via `settings_store_windows.go`) → explicit CLI flags. The GUI writes settings on Start. Adding a tunable means touching `config.go` (struct + flag + validation), `settings_store_*.go` (`persistedSettings` + load/save), and the GUI form.

## Firmware architecture

Modules under `namespace bridge`, each a `.h`/`.cpp` pair compiled by Arduino as separate translation units:
- `firmware.ino` — `setup()`/`loop()`; sets BLE device/manufacturer name and starts `Keyboard`/`Mouse`.
- `serial_processor.cpp` — byte-at-a-time line assembler with a fixed 96-byte buffer; oversized lines are dropped until the next `\n` to resync (never blocks/desyncs).
- `command_dispatch.cpp` — splits a line into command+args and dispatches. All actions no-op unless `Keyboard.isConnected()`. Mouse deltas larger than int8 are chunked into ±127 steps (`sendChunkedMove`) because HID reports are signed-byte deltas.
- `connection_led.cpp` — built-in LED: solid when no BLE client, a 200 ms pulse every 20 s when connected.
- `bridge_types.h` — shared constants (`kSerialBaud` 115200, `kMaxLineLength`, HID delta range).

## Conventions

- Go: build constraints are load-bearing — keep `//go:build windows` on `software/` files and the `_darwin.go` suffix on macOS-only files, or cross-package builds break.
- Firmware: keep everything in `namespace bridge`; per-command logic stays as small `executeXxx` helpers in `command_dispatch.cpp`.
- Baud is hard-coded to `115200` on both ends (`bridge_types.h` and the `-baud` default) — changing one without the other breaks the link.

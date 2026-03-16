# ESP HID Bridge

ESP HID Bridge lets a Windows PC forward mouse and keyboard input to an ESP32 over USB serial, then the ESP32 re-emits it as Bluetooth LE HID (mouse + keyboard) to a paired phone/tablet.

## Highlights

- Windows-native sender written in Go.
- ESP32 firmware using BLE Combo HID.
- GUI mode (default) with system tray behavior.
- CLI mode for terminal-only workflows.
- Auto serial reconnect and safety key/button release.
- Remote mode toggle hotkey (default F9), gated by serial connection health.

## Repository Layout

- `firmware/`: Arduino sketch and serial command parser for ESP32.
- `firmware/libraries/ESP32-BLE-Combo/`: bundled BLE Combo library copy.
- `software/`: Windows sender app (hooks, serial runtime, GUI, tray integration).
- `software/main.go`: program entrypoint (`-gui=true` by default).

## How It Works

1. The Windows app installs low-level input hooks.
2. In remote mode, mouse/keyboard activity is converted into compact text commands.
3. Commands are sent over USB serial to the ESP32.
4. ESP32 parses commands and emits BLE HID reports to the paired target device.

## Requirements

### Firmware Side

- ESP32 development board.
- Arduino IDE 2.x.
- Espressif ESP32 board package.

### Software Side

- Windows 10/11.
- Go 1.22+.
- USB serial connection to ESP32.

## Firmware Setup (ESP32)

1. Open `firmware/firmware.ino` in Arduino IDE.
2. Select your ESP32 board and COM port.
3. Ensure BLE Combo library is available.
	 - The repo already includes a local copy under `firmware/libraries/ESP32-BLE-Combo/`.
	 - The bundled `ESP32-BLE-Combo` copy is already patched for the latest ESP32 `3.x.x`.
4. Build and upload.

Default firmware values:

- Serial baud: `230400`.
- BLE device name: `PC Bridge Combo`.
- BLE manufacturer: `ESP HID Bridge`.

### Build/Flash With Arduino CLI

From `firmware/`:

```powershell
# Build firmware into firmware\out\
arduino-cli compile --fqbn esp32:esp32:esp32 --libraries libraries --output-dir out .

# Flash previously built firmware (replace COM9 with your ESP32 port)
arduino-cli upload -p COM9 -b esp32:esp32:esp32 --input-dir out -t
```

## Software Setup (Windows)

From `software/`:

```powershell
go mod tidy
# Production GUI build (no terminal window when opening the EXE)
go build -trimpath -ldflags "-H=windowsgui -s -w" -o esp-hid-bridge.exe .
```

Optional helper script:

```powershell
.\build-production.ps1
```

If you run `go build .`, Go produces a console-subsystem EXE (`software.exe`) which opens a terminal window.

Run GUI mode (default):

```powershell
.\esp-hid-bridge.exe
```

Run CLI mode:

```powershell
.\esp-hid-bridge.exe -gui=false -port auto
```

## GUI Behavior

- App starts hidden in system tray.
- Left-click tray icon opens the main window.
- Closing the window hides it to tray.
- Tray menu `Exit` fully terminates the app.

Bridge status in GUI:

- Green text: connected.
- Amber text: transitional/waiting state (starting/stopping/stopped).
- Red text: connection/capture failure.

## Remote Mode Behavior

- Remote mode can be activated by:
	- moving cursor to the host-side boundary (right edge when `-host-side=left`, left edge when `-host-side=right`, etc.), or
	- pressing toggle hotkey (default `F9`, configurable `F1`-`F12`).
- Host return always works via the toggle hotkey.
- Edge-aware return can be configured with slave resolution and host-side placement settings (GUI dropdowns or CLI flags). With these set, return to host happens only when you push against the configured host-side edge of the virtual slave surface.
- Optional left-swipe return can also be enabled (`-leftreturn=true`) as a fallback gesture.
- Toggle hotkey only works when serial connection is healthy.
- If serial drops while remote mode is active, remote mode is disabled and release commands are sent.

## Command-Line Flags

All flags apply to both GUI and CLI modes:

- `-port`: serial port or `auto` (default `auto`).
- `-baud`: serial baud rate (default `230400`).
- `-rate`: movement send rate Hz (default `45`).
- `-deadzone`: ignore tiny move deltas up to this absolute value (default `1`, `0` disables).
- `-smooth`: micro-smoothing factor for small movement (default `0.2`, range `[0, 1)`, `0` disables).
- `-adaptive`: adapt move send cadence when serial queue is congested (default `true`).
- `-slave-res`: virtual slave resolution `WIDTHxHEIGHT` for edge-aware return (default `1920x1080`).
	- GUI includes common laptop and mobile/tablet presets and also accepts custom `WIDTHxHEIGHT` values.
- `-host-side`: host placement relative to slave (`left|right|top|bottom`, default `left`).
- `-leftreturn`: allow host return by deliberate quick left-swipe in remote mode (default `false`).
- `-reconnect`: reconnect delay after serial failure (default `750ms`).
- `-keyboard`: forward keyboard events (default `true`).
- `-toggle`: remote mode hotkey (`F1`-`F12`, default `F9`).
- `-gui`: launch native GUI (`true`) or CLI (`false`).

## Serial Protocol

Commands are newline-delimited UTF-8 text.

Supported commands:

- `MOVE dx dy`
- `MOUSEDOWN LEFT|RIGHT|MIDDLE`
- `MOUSEUP LEFT|RIGHT|MIDDLE`
- `CLICK LEFT|RIGHT|MIDDLE`
- `SCROLL amount`
- `KEYDOWN code`
- `KEYUP code`
- `KEYRELEASE`
- `RELEASE`

Notes:

- HID deltas are chunked to int8 range (`-127..127`) by firmware.
- Oversized serial lines are dropped safely until newline resync.

## End-to-End Quick Start

1. Flash firmware to ESP32.
2. Pair target phone/tablet with BLE device `PC Bridge Combo`.
3. Connect ESP32 to Windows via USB.
4. Run sender app on Windows.
5. In GUI, click `Start` and confirm Bridge status becomes connected.
6. Use hotkey/edge to enter remote mode and control the paired device.

## Development (Optional)

From `software/` with Air:

```powershell
go install github.com/air-verse/air@latest
air -c .air.toml
```

## Troubleshooting

- Bridge never reaches connected:
	- verify ESP32 firmware is running and USB cable supports data.
	- try `-port auto` or explicitly set `-port COMx`.
- BLE control not working after firmware changes:
	- remove old Bluetooth pairing and pair again.
- Hotkey does nothing:
	- expected when serial connection is down; reconnect first.


# host — Windows sender for firmware-idf

Go rewrite of the PC-side app, paired with `../firmware-idf/`. Windows-first;
the platform-independent packages (`protocol`, `device`, `core`, `config`,
`keymap`, `hotkey`) build and test on any OS, and a `-cli` diagnostics mode
runs anywhere.

What's new over the legacy `software/`:

- **Zero-config device discovery**: finds the ESP32-C3 by USB VID/PID
  `303A:1001` — no COM-port guessing, no port picker.
- **Binary, bidirectional protocol**: the GUI shows the device's BLE state
  ("Advertising — pair the phone…" / "Connected") and firmware version, and
  has a **Clear device bonds** button for stale-pairing recovery.
- Remote mode requires serial **and** BLE connected; it force-exits if
  either drops (you can't get trapped controlling an unreachable device).
- Middle mouse button and horizontal scroll now forwarded.
- Settings survive schema growth (`%AppData%\ESP HID Bridge\settings-v2.json`).

## Build

On Windows:

```powershell
cd host
go mod tidy
./build-production.ps1                # GUI exe, embedded icons
./esp-hid-bridge.exe                  # GUI (default)
./esp-hid-bridge.exe -gui=false      # headless capture, console logs
./esp-hid-bridge.exe -cli            # diagnostics only (no capture)
```

Or download `esp-hid-bridge.exe` from GitHub Releases (built by
`.github/workflows/release-v2.yml`, manual dispatch).

Cross-compile check from macOS/Linux: `GOOS=windows GOARCH=amd64 go build ./...`

## Test

```bash
go vet ./... && go test ./...
```

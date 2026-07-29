# firmware-idf — ESP-IDF firmware (ESP32-C3)

Ground-up ESP-IDF v6.0 rewrite of the bridge firmware. Replaces the Arduino
sketch in `firmware/` with proper ESP components:

- **BLE HID**: ESP-IDF `esp_hid` device role over **NimBLE** — combined
  keyboard (report ID 1) + mouse with wheel/pan (report ID 3). Just Works
  pairing, bonded, LE Secure Connections. Advertises as `ESP-HID-ME`
  (configurable via `idf.py menuconfig` → ESP-HID Bridge).
- **Transport**: binary framed protocol (see `docs/PROTOCOL.md`) over the
  C3's native USB Serial/JTAG CDC port. **Bidirectional** — the device
  reports firmware version, BLE state transitions, and errors, and accepts a
  bond-wipe command. No more black-box debugging.
- Auto-recovering advertising (retries after failed connections), stale-bond
  self-healing (`REPEAT_PAIRING` → delete + retry), low-latency connection
  parameters (7.5–15 ms).
- Status LED (GPIO8, active-low, SuperMini default): solid = waiting for
  phone, short pulse every 20 s = connected.

## Build & flash

```bash
get_idf                     # or: . $IDF_PATH/export.sh
idf.py set-target esp32c3   # first time only
idf.py build
idf.py -p /dev/cu.usbmodem* flash
```

Logs go to UART0 (GPIO21 TX / GPIO20 RX @ 115200), NOT the USB port — the
USB port carries the binary protocol. For log access without a UART dongle,
enable `BRIDGE_LOG_FRAMES` in menuconfig: log lines are then mirrored as
protocol LOG frames.

## Testing without the Windows app

```bash
tools/.venv/bin/python tools/hidctl.py status        # HELLO + BLE state
tools/.venv/bin/python tools/hidctl.py watch         # live state + logs
tools/.venv/bin/python tools/hidctl.py move-square   # wiggle the cursor
tools/.venv/bin/python tools/hidctl.py type "hello"
tools/.venv/bin/python tools/hidctl.py clear-bonds
```

(Once: `python3 -m venv tools/.venv && tools/.venv/bin/pip install pyserial`.)

Off-target protocol codec test:

```bash
cc -Wall -Wextra -Werror -std=c11 -Imain tools/test_protocol.c main/protocol.c -o /tmp/tp && /tmp/tp
```

The Go host app for this firmware lives in `../host/`.

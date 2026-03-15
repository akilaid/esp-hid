# esp-hid

Cross-device mouse and keyboard bridge using an ESP32 as a Bluetooth HID device.

The Windows app captures global mouse and keyboard input and sends compact serial commands to the ESP32. The ESP32 forwards them as BLE HID reports to a paired phone or tablet.

## Project Layout

- `firmware/firmware.ino`: ESP32 Arduino firmware (BLE HID combo mouse + keyboard + serial parser)
- `software/main.go`: Windows sender (global mouse/keyboard hooks + serial bridge)

## Serial Protocol

Commands are newline-delimited UTF-8 text:

- `MOVE dx dy`
- `CLICK LEFT`
- `CLICK RIGHT`
- `SCROLL amount`
- `KEYDOWN code`
- `KEYUP code`
- `KEYRELEASE`
- `RELEASE`

Examples:

- `MOVE 5 -3`
- `SCROLL -1`
- `KEYDOWN 97` (`a`)
- `KEYUP 97`

## Firmware (ESP32 Arduino)

### Requirements

- ESP32 development board
- Arduino IDE 2.x
- ESP32 board package installed in Arduino IDE
- Library: `ESP32 BLE Combo` (BleCombo)

### ESP32 Core 3.x Compatibility Patch

If you use Espressif core 3.x, patch two lines in `BleComboKeyboard.cpp`:

- `BLEDevice::init(bleKeyboardInstance->deviceName);`
	-> `BLEDevice::init(String(bleKeyboardInstance->deviceName.c_str()));`
- `bleKeyboardInstance->hid->manufacturer()->setValue(bleKeyboardInstance->deviceManufacturer);`
	-> `bleKeyboardInstance->hid->manufacturer()->setValue(String(bleKeyboardInstance->deviceManufacturer.c_str()));`

### Flash Steps

1. Open `firmware/firmware.ino` in Arduino IDE.
2. Select your ESP32 board and COM port.
3. Install `ESP32 BLE Combo`:
	- Download ZIP from `https://github.com/blackketter/ESP32-BLE-Combo`.
	- Arduino IDE -> Sketch -> Include Library -> Add .ZIP Library...
4. Build and upload.
5. After boot, the ESP32 advertises as `PC Bridge Combo`.

## Software (Windows Go Sender)

### Requirements

- Windows 10/11
- Go 1.22+
- ESP32 connected via USB serial

### Build

From `software/`:

```powershell
go mod tidy
go build -o mousebridge.exe .
```

### Run

```powershell
.\mousebridge.exe -port COM9 -baud 115200 -rate 120
```

or use automatic port selection:

```powershell
.\mousebridge.exe -port auto -baud 115200 -rate 120
```

To disable keyboard forwarding:

```powershell
.\mousebridge.exe -port auto -keyboard=false
```

Flags:

- `-port`: Serial port or `auto` (default `auto`)
- `-baud`: Baud rate (default `115200`)
- `-rate`: Max movement send rate in Hz (default `120`)
- `-reconnect`: Reconnect delay after serial failure (default `750ms`)
- `-keyboard`: Forward keyboard events (default `true`)

## End-to-End Workflow

1. Connect ESP32 to the Windows PC over USB.
2. Pair your phone/tablet with Bluetooth device `PC Bridge Combo`.
3. Start `mousebridge.exe` on Windows.
4. Move/click/scroll with the PC mouse and type on the PC keyboard to control the paired device.

## Notes

- The Windows app automatically reconnects when the serial link drops.
- Movement is rate-limited and accumulated to keep latency low while avoiding command floods.
- On serial reconnect, the software sends `RELEASE` and `KEYRELEASE` to avoid stuck buttons/keys.
- Press `Ctrl+C` in the Windows app to stop; it sends `RELEASE` and `KEYRELEASE` before exit.
- If you switch firmware from mouse-only to combo mode, remove old Bluetooth pairing first (cached HID descriptors can cause wrong behavior).


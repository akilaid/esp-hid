# ESP-HID Bridge Wire Protocol v1

Binary framed protocol between the host PC and the ESP32-C3 over the native
USB Serial/JTAG CDC port (USB VID/PID `303A:1001`). Replaces the newline-text
protocol of the legacy `firmware/` + `software/` pair. Bidirectional: the
device reports its identity, BLE state, and errors, so every failure mode is
observable from the host.

The CDC link ignores baud settings (no real UART), so any baud works; hosts
should still set a sane value (e.g. 115200) for driver compatibility.

Opening or closing the CDC port does **not** reset the C3 (no DTR/RTS reset
circuit on native USB). Device state persists across host reconnects; a host
must send `GET_STATUS` after opening the port to resynchronize.

## Frame format

```
+------+------+------+-----+----------------+-------+
| 0xAA | 0x55 | type | len | payload[len]   | crc8  |
+------+------+------+-----+----------------+-------+
```

- `type` — message type, one byte. Host→device types are `< 0x80`; device→host
  types have the top bit set.
- `len` — payload length, `0..32`. Frames with `len > 32` are invalid.
- `crc8` — CRC-8, polynomial `0x07` (CRC-8/CCITT-style: init `0x00`, no
  reflection, no final XOR), computed over `type | len | payload`.
  The sync bytes are NOT covered.
- All multi-byte payload integers are **little-endian**.

USB transit is already CRC16-protected per packet; this CRC exists to reject
desync garbage (partial frames after reconnects or buffer overruns), for which
8 bits plus the 2-byte sync and the len bound is ample.

### Decoder state machine and resync

States: `WAIT_AA → WAIT_55 → TYPE → LEN → PAYLOAD → CRC`.

- In `WAIT_AA`, any byte other than `0xAA` is discarded.
- In `WAIT_55`, `0xAA` stays in `WAIT_55` (run of sync bytes), any other
  non-`0x55` byte returns to `WAIT_AA`.
- `len > 32` → error `bad_len`, return to `WAIT_AA`.
- CRC mismatch → error `bad_crc`, return to `WAIT_AA`.

A false `AA 55` inside a payload costs at most one `bad_crc` rejection, after
which the stream self-heals: the 2-byte sync pattern relocks within one frame.

### Link liveness

The host sends `PING` once per second. Three consecutive missed `PONG`s mean
the link is dead: the host closes and reopens the port. The device never
times out the host; it just answers what it receives.

## Messages: host → device

| type | name        | payload                                         |
|------|-------------|-------------------------------------------------|
| 0x01 | PING        | `u32 nonce`                                     |
| 0x02 | GET_STATUS  | — (device replies HELLO then BLE_STATE)         |
| 0x10 | MOVE        | `i16 dx, i16 dy` (positive = right/down)        |
| 0x11 | BUTTONS     | `u8 mask` — absolute button state               |
| 0x12 | WHEEL       | `i8 vertical, i8 horizontal` (pos = up / right) |
| 0x13 | KEY_DOWN    | `u8 usage` — HID Keyboard/Keypad page usage     |
| 0x14 | KEY_UP      | `u8 usage`                                      |
| 0x15 | RELEASE_ALL | — zero both reports (all buttons + keys up)     |
| 0x20 | CLEAR_BONDS | — delete all BLE bonds; device replies ACK      |

- **BUTTONS mask**: bit0 = left, bit1 = right, bit2 = middle, bit3 = back,
  bit4 = forward. The state is absolute, not down/up events, so a dropped
  frame is corrected by the next one.
- **KEY_DOWN/KEY_UP usages** are raw USB HID Keyboard/Keypad page (0x07)
  usage IDs. Usages `0xE0..0xE7` are the modifier keys (LCtrl, LShift, LAlt,
  LGUI, RCtrl, RShift, RAlt, RGUI) and set/clear the corresponding modifier
  bit instead of occupying a key slot. There is no ASCII mapping and no
  auto-shift: the host sends the exact keys the user pressed.
- **MOVE** deltas may exceed ±127; the device chunks them into int8 HID
  reports itself.
- Input messages (0x10–0x15) are silently dropped while no BLE host is
  connected (matching legacy behavior). The device MAY rate-limited-report
  this via ERROR `not_connected_drop` for diagnosis.

## Messages: device → host

| type | name      | payload                                                  |
|------|-----------|----------------------------------------------------------|
| 0x81 | HELLO     | `u8 proto_ver, u8 fw_major, u8 fw_minor, u8 fw_patch, u16 caps` |
| 0x82 | BLE_STATE | `u8 state, u8 reason, u8 bond_count`                     |
| 0x83 | ACK       | `u8 acked_type`                                          |
| 0x84 | ERROR     | `u8 code, u8 detail`                                     |
| 0x85 | PONG      | `u32 nonce` (echoed)                                     |
| 0x86 | LOG       | UTF-8 text, ≤32 bytes (dev builds only)                  |

- **HELLO** is sent once on boot and in reply to every `GET_STATUS`.
  `proto_ver` = 1. `caps` bit0 = mouse, bit1 = keyboard (both set).
- **BLE_STATE** is sent on every state change and after HELLO in the
  `GET_STATUS` reply. `state`: 0 = idle (BLE down), 1 = advertising,
  2 = connected. `reason` = BLE disconnect reason of the most recent
  disconnect (0 if none). `bond_count` = number of stored bonds.
- **ERROR codes**: 1 = bad_crc, 2 = unknown_type, 3 = bad_len,
  4 = hid_send_fail, 5 = not_connected_drop. Rate-limited to at most one
  per second per code.

## Test vectors

CRC-8 poly 0x07, init 0x00 (over `type|len|payload`):

| bytes (hex)                | crc8 |
|----------------------------|------|
| `01 04 78 56 34 12`        | 0xAE |
| `02 00`                    | 0x2A |
| `10 04 05 00 FB FF`        | 0x2F |
| `15 00`                    | 0x16 |
| `81 06 01 01 00 00 03 00`  | 0x14 |

Complete example frames:

- `PING nonce=0x12345678` → `AA 55 01 04 78 56 34 12 AE`
- `GET_STATUS` → `AA 55 02 00 2A`
- `MOVE dx=5 dy=-5` → `AA 55 10 04 05 00 FB FF 2F`
- `RELEASE_ALL` → `AA 55 15 00 16`
- `HELLO v1 fw1.0.0 caps=3` → `AA 55 81 06 01 01 00 00 03 00 14`

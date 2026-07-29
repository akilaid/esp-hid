#!/usr/bin/env python3
"""hidctl — test client for the ESP-HID bridge binary protocol.

Speaks the protocol from firmware-idf/docs/PROTOCOL.md over the ESP32-C3's
USB Serial/JTAG CDC port. Requires pyserial.

Usage:
  hidctl.py [--port PORT] status            # HELLO + BLE_STATE
  hidctl.py [--port PORT] ping              # round-trip latency
  hidctl.py [--port PORT] watch             # dump device frames until Ctrl-C
  hidctl.py [--port PORT] move-square       # trace a square with the cursor
  hidctl.py [--port PORT] type TEXT         # type ASCII text on the phone
  hidctl.py [--port PORT] buttons MASK      # set absolute button mask (int)
  hidctl.py [--port PORT] wheel V [H]       # scroll
  hidctl.py [--port PORT] release           # release all buttons + keys
  hidctl.py [--port PORT] clear-bonds       # wipe BLE bonds (needs re-pair)

Without --port the first port with USB VID:PID 303A:1001 is used.
"""

import argparse
import sys
import time

try:
    import serial
    from serial.tools import list_ports
except ImportError:  # codec still usable without pyserial; ports are not
    serial = None
    list_ports = None

SYNC1, SYNC2 = 0xAA, 0x55
MAX_PAYLOAD = 32

# Host -> device
PING, GET_STATUS = 0x01, 0x02
MOVE, BUTTONS, WHEEL, KEY_DOWN, KEY_UP, RELEASE_ALL = 0x10, 0x11, 0x12, 0x13, 0x14, 0x15
CLEAR_BONDS = 0x20
# Device -> host
HELLO, BLE_STATE, ACK, ERROR, PONG, LOG = 0x81, 0x82, 0x83, 0x84, 0x85, 0x86

TYPE_NAMES = {
    HELLO: "HELLO", BLE_STATE: "BLE_STATE", ACK: "ACK",
    ERROR: "ERROR", PONG: "PONG", LOG: "LOG",
}
BLE_STATES = {0: "idle", 1: "advertising", 2: "connected"}
ERROR_CODES = {1: "bad_crc", 2: "unknown_type", 3: "bad_len",
               4: "hid_send_fail", 5: "not_connected_drop"}

ESP_VID, ESP_PID = 0x303A, 0x1001

# HID usage for ASCII typing (US layout): char -> (usage, needs_shift)
_LETTERS = {chr(ord('a') + i): (0x04 + i, False) for i in range(26)}
_LETTERS.update({chr(ord('A') + i): (0x04 + i, True) for i in range(26)})
_DIGITS = {"1": (0x1E, False), "2": (0x1F, False), "3": (0x20, False),
           "4": (0x21, False), "5": (0x22, False), "6": (0x23, False),
           "7": (0x24, False), "8": (0x25, False), "9": (0x26, False),
           "0": (0x27, False)}
_OTHER = {
    " ": (0x2C, False), "\n": (0x28, False), "\t": (0x2B, False),
    "-": (0x2D, False), "_": (0x2D, True), "=": (0x2E, False), "+": (0x2E, True),
    "[": (0x2F, False), "{": (0x2F, True), "]": (0x30, False), "}": (0x30, True),
    "\\": (0x31, False), "|": (0x31, True), ";": (0x33, False), ":": (0x33, True),
    "'": (0x34, False), '"': (0x34, True), "`": (0x35, False), "~": (0x35, True),
    ",": (0x36, False), "<": (0x36, True), ".": (0x37, False), ">": (0x37, True),
    "/": (0x38, False), "?": (0x38, True),
    "!": (0x1E, True), "@": (0x1F, True), "#": (0x20, True), "$": (0x21, True),
    "%": (0x22, True), "^": (0x23, True), "&": (0x24, True), "*": (0x25, True),
    "(": (0x26, True), ")": (0x27, True),
}
ASCII_TO_USAGE = {**_LETTERS, **_DIGITS, **_OTHER}
USAGE_LSHIFT = 0xE1


def crc8(data: bytes) -> int:
    crc = 0
    for byte in data:
        crc ^= byte
        for _ in range(8):
            crc = ((crc << 1) ^ 0x07 if crc & 0x80 else crc << 1) & 0xFF
    return crc


def encode(ftype: int, payload: bytes = b"") -> bytes:
    assert len(payload) <= MAX_PAYLOAD
    body = bytes([ftype, len(payload)]) + payload
    return bytes([SYNC1, SYNC2]) + body + bytes([crc8(body)])


class Decoder:
    """Incremental decoder mirroring the Go/C state machine."""

    def __init__(self):
        self.state = "AA"
        self.ftype = 0
        self.need = 0
        self.buf = bytearray()

    def feed(self, byte: int):
        if self.state == "AA":
            if byte == SYNC1:
                self.state = "55"
        elif self.state == "55":
            if byte == SYNC2:
                self.state = "TYPE"
            elif byte != SYNC1:
                self.state = "AA"
        elif self.state == "TYPE":
            self.ftype = byte
            self.state = "LEN"
        elif self.state == "LEN":
            if byte > MAX_PAYLOAD:
                self.state = "AA"
                return None
            self.need = byte
            self.buf = bytearray()
            self.state = "PAYLOAD" if byte else "CRC"
        elif self.state == "PAYLOAD":
            self.buf.append(byte)
            if len(self.buf) == self.need:
                self.state = "CRC"
        elif self.state == "CRC":
            self.state = "AA"
            body = bytes([self.ftype, self.need]) + bytes(self.buf)
            if crc8(body) == byte:
                return self.ftype, bytes(self.buf)
        return None


def describe(ftype: int, payload: bytes) -> str:
    name = TYPE_NAMES.get(ftype, f"0x{ftype:02X}")
    if ftype == HELLO and len(payload) >= 6:
        caps = payload[4] | payload[5] << 8
        return (f"HELLO proto={payload[0]} fw={payload[1]}.{payload[2]}.{payload[3]}"
                f" caps=0x{caps:04X}")
    if ftype == BLE_STATE and len(payload) >= 3:
        return (f"BLE_STATE {BLE_STATES.get(payload[0], payload[0])}"
                f" reason=0x{payload[1]:02X} bonds={payload[2]}")
    if ftype == ACK and payload:
        return f"ACK for 0x{payload[0]:02X}"
    if ftype == ERROR and len(payload) >= 2:
        return f"ERROR {ERROR_CODES.get(payload[0], payload[0])} detail=0x{payload[1]:02X}"
    if ftype == PONG and len(payload) >= 4:
        return f"PONG nonce=0x{int.from_bytes(payload[:4], 'little'):08X}"
    if ftype == LOG:
        return f"LOG {payload.decode('utf-8', 'replace')}"
    return f"{name} {payload.hex(' ')}"


def find_port() -> str:
    for port in list_ports.comports():
        if port.vid == ESP_VID and port.pid == ESP_PID:
            return port.device
    sys.exit(f"no port with USB ID {ESP_VID:04X}:{ESP_PID:04X} found "
             f"(available: {', '.join(p.device for p in list_ports.comports()) or 'none'})")


def open_link(port_name):
    if serial is None:
        sys.exit("pyserial is required: pip3 install pyserial")
    port = serial.Serial(port_name or find_port(), 115200, timeout=0.1)
    return port, Decoder()


def pump(port, decoder, duration, on_frame):
    """Read for `duration` seconds, calling on_frame(type, payload). on_frame
    may return True to stop early."""
    deadline = time.monotonic() + duration
    while time.monotonic() < deadline:
        data = port.read(256)
        for byte in data:
            frame = decoder.feed(byte)
            if frame and on_frame(*frame):
                return True
    return False


def wait_for(port, decoder, wanted_type, duration=2.0):
    result = []

    def on_frame(ftype, payload):
        print(" ", describe(ftype, payload))
        if ftype == wanted_type:
            result.append((ftype, payload))
            return True
        return False

    pump(port, decoder, duration, on_frame)
    return result[0] if result else None


def cmd_status(port, decoder, _args):
    port.write(encode(GET_STATUS))
    if not wait_for(port, decoder, BLE_STATE):
        sys.exit("no BLE_STATE reply — is the new firmware flashed?")


def cmd_ping(port, decoder, _args):
    nonce = 0x12345678
    start = time.monotonic()
    port.write(encode(PING, nonce.to_bytes(4, "little")))
    reply = wait_for(port, decoder, PONG)
    if not reply:
        sys.exit("no PONG")
    elapsed = (time.monotonic() - start) * 1000
    echoed = int.from_bytes(reply[1][:4], "little")
    status = "ok" if echoed == nonce else f"NONCE MISMATCH 0x{echoed:08X}"
    print(f"  round-trip {elapsed:.1f} ms ({status})")


def cmd_watch(port, decoder, _args):
    print("watching (Ctrl-C to stop)...")
    port.write(encode(GET_STATUS))
    try:
        while True:
            pump(port, decoder, 3600, lambda t, p: print(" ", describe(t, p)))
    except KeyboardInterrupt:
        pass


def cmd_move_square(port, decoder, _args):
    print("tracing a 200px square...")
    for dx, dy in [(200, 0), (0, 200), (-200, 0), (0, -200)]:
        step_x = dx // 20
        step_y = dy // 20
        for _ in range(20):
            payload = (step_x.to_bytes(2, "little", signed=True)
                       + step_y.to_bytes(2, "little", signed=True))
            port.write(encode(MOVE, payload))
            time.sleep(0.01)
    pump(port, decoder, 0.5, lambda t, p: print(" ", describe(t, p)))


def cmd_type(port, decoder, args):
    text = args.text
    for char in text:
        mapping = ASCII_TO_USAGE.get(char)
        if not mapping:
            print(f"  skipping unmappable {char!r}")
            continue
        usage, shift = mapping
        if shift:
            port.write(encode(KEY_DOWN, bytes([USAGE_LSHIFT])))
        port.write(encode(KEY_DOWN, bytes([usage])))
        port.write(encode(KEY_UP, bytes([usage])))
        if shift:
            port.write(encode(KEY_UP, bytes([USAGE_LSHIFT])))
        time.sleep(0.02)
    pump(port, decoder, 0.5, lambda t, p: print(" ", describe(t, p)))


def cmd_buttons(port, decoder, args):
    port.write(encode(BUTTONS, bytes([args.mask & 0xFF])))
    pump(port, decoder, 0.3, lambda t, p: print(" ", describe(t, p)))


def cmd_wheel(port, decoder, args):
    payload = bytes([args.vertical & 0xFF, args.horizontal & 0xFF])
    port.write(encode(WHEEL, payload))
    pump(port, decoder, 0.3, lambda t, p: print(" ", describe(t, p)))


def cmd_release(port, decoder, _args):
    port.write(encode(RELEASE_ALL))
    pump(port, decoder, 0.3, lambda t, p: print(" ", describe(t, p)))


def cmd_clear_bonds(port, decoder, _args):
    port.write(encode(CLEAR_BONDS))
    if wait_for(port, decoder, ACK, 3.0):
        print("  bonds cleared — forget the device on the phone and re-pair")
    else:
        sys.exit("no ACK")


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--port", help="serial port (default: auto by VID/PID)")
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("status")
    sub.add_parser("ping")
    sub.add_parser("watch")
    sub.add_parser("move-square")
    type_parser = sub.add_parser("type")
    type_parser.add_argument("text")
    buttons_parser = sub.add_parser("buttons")
    buttons_parser.add_argument("mask", type=lambda v: int(v, 0))
    wheel_parser = sub.add_parser("wheel")
    wheel_parser.add_argument("vertical", type=int)
    wheel_parser.add_argument("horizontal", type=int, nargs="?", default=0)
    sub.add_parser("release")
    sub.add_parser("clear-bonds")
    args = parser.parse_args()

    handlers = {
        "status": cmd_status, "ping": cmd_ping, "watch": cmd_watch,
        "move-square": cmd_move_square, "type": cmd_type,
        "buttons": cmd_buttons, "wheel": cmd_wheel,
        "release": cmd_release, "clear-bonds": cmd_clear_bonds,
    }
    port, decoder = open_link(args.port)
    try:
        handlers[args.command](port, decoder, args)
    finally:
        port.close()


if __name__ == "__main__":
    main()

// Package protocol implements the ESP-HID bridge binary wire protocol.
// See firmware-idf/docs/PROTOCOL.md for the authoritative specification.
package protocol

import (
	"encoding/binary"
	"fmt"
)

// Frame layout: 0xAA 0x55 | type | len | payload[len] | crc8.
const (
	SyncByte1 = 0xAA
	SyncByte2 = 0x55

	// MaxPayload is the largest payload a frame may carry.
	MaxPayload = 32

	// MaxFrame is the encoded size of a maximal frame.
	MaxFrame = 2 + 1 + 1 + MaxPayload + 1

	// Version is the protocol version this package implements.
	Version = 1
)

// Host → device message types.
const (
	TypePing       = 0x01
	TypeGetStatus  = 0x02
	TypeMove       = 0x10
	TypeButtons    = 0x11
	TypeWheel      = 0x12
	TypeKeyDown    = 0x13
	TypeKeyUp      = 0x14
	TypeReleaseAll = 0x15
	TypeClearBonds = 0x20
)

// Device → host message types.
const (
	TypeHello    = 0x81
	TypeBleState = 0x82
	TypeAck      = 0x83
	TypeError    = 0x84
	TypePong     = 0x85
	TypeLog      = 0x86
)

// BLE states carried by BLE_STATE.
const (
	BleIdle        = 0
	BleAdvertising = 1
	BleConnected   = 2
)

// ERROR codes carried by device ERROR frames.
const (
	ErrBadCRC           = 1
	ErrUnknownType      = 2
	ErrBadLen           = 3
	ErrHidSendFail      = 4
	ErrNotConnectedDrop = 5
)

// Button mask bits for BUTTONS frames.
const (
	ButtonLeft    = 1 << 0
	ButtonRight   = 1 << 1
	ButtonMiddle  = 1 << 2
	ButtonBack    = 1 << 3
	ButtonForward = 1 << 4
)

// CRC8 computes CRC-8 poly 0x07, init 0x00, no reflection, no final XOR.
func CRC8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// Frame is one decoded protocol frame.
type Frame struct {
	Type    byte
	Payload []byte
}

// Encode serializes a frame. It panics if payload exceeds MaxPayload, which
// indicates a programming error: every message this package defines fits.
func Encode(frameType byte, payload []byte) []byte {
	if len(payload) > MaxPayload {
		panic(fmt.Sprintf("protocol: payload %d exceeds max %d", len(payload), MaxPayload))
	}
	out := make([]byte, 0, 4+len(payload)+1)
	out = append(out, SyncByte1, SyncByte2, frameType, byte(len(payload)))
	out = append(out, payload...)
	out = append(out, CRC8(out[2:]))
	return out
}

// Message constructors — host → device.

func EncodePing(nonce uint32) []byte {
	var p [4]byte
	binary.LittleEndian.PutUint32(p[:], nonce)
	return Encode(TypePing, p[:])
}

func EncodeGetStatus() []byte { return Encode(TypeGetStatus, nil) }

func EncodeMove(dx, dy int16) []byte {
	var p [4]byte
	binary.LittleEndian.PutUint16(p[0:], uint16(dx))
	binary.LittleEndian.PutUint16(p[2:], uint16(dy))
	return Encode(TypeMove, p[:])
}

func EncodeButtons(mask byte) []byte { return Encode(TypeButtons, []byte{mask}) }

func EncodeWheel(vertical, horizontal int8) []byte {
	return Encode(TypeWheel, []byte{byte(vertical), byte(horizontal)})
}

func EncodeKeyDown(usage byte) []byte { return Encode(TypeKeyDown, []byte{usage}) }
func EncodeKeyUp(usage byte) []byte   { return Encode(TypeKeyUp, []byte{usage}) }
func EncodeReleaseAll() []byte        { return Encode(TypeReleaseAll, nil) }
func EncodeClearBonds() []byte        { return Encode(TypeClearBonds, nil) }

// Typed views of device → host frames.

// Hello is the parsed HELLO payload.
type Hello struct {
	ProtoVersion byte
	FwMajor      byte
	FwMinor      byte
	FwPatch      byte
	Caps         uint16
}

// BleState is the parsed BLE_STATE payload.
type BleState struct {
	State     byte // BleIdle, BleAdvertising, BleConnected
	Reason    byte // most recent disconnect reason, 0 if none
	BondCount byte
}

// ParseHello decodes a HELLO frame payload.
func ParseHello(f Frame) (Hello, error) {
	if f.Type != TypeHello || len(f.Payload) < 6 {
		return Hello{}, fmt.Errorf("protocol: not a valid HELLO frame (type 0x%02X len %d)", f.Type, len(f.Payload))
	}
	return Hello{
		ProtoVersion: f.Payload[0],
		FwMajor:      f.Payload[1],
		FwMinor:      f.Payload[2],
		FwPatch:      f.Payload[3],
		Caps:         binary.LittleEndian.Uint16(f.Payload[4:6]),
	}, nil
}

// ParseBleState decodes a BLE_STATE frame payload.
func ParseBleState(f Frame) (BleState, error) {
	if f.Type != TypeBleState || len(f.Payload) < 3 {
		return BleState{}, fmt.Errorf("protocol: not a valid BLE_STATE frame (type 0x%02X len %d)", f.Type, len(f.Payload))
	}
	return BleState{State: f.Payload[0], Reason: f.Payload[1], BondCount: f.Payload[2]}, nil
}

// ParsePong extracts the echoed nonce from a PONG frame.
func ParsePong(f Frame) (uint32, error) {
	if f.Type != TypePong || len(f.Payload) < 4 {
		return 0, fmt.Errorf("protocol: not a valid PONG frame (type 0x%02X len %d)", f.Type, len(f.Payload))
	}
	return binary.LittleEndian.Uint32(f.Payload[:4]), nil
}

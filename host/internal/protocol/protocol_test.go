package protocol

import (
	"bytes"
	"math/rand"
	"testing"
)

// Spec test vectors from firmware-idf/docs/PROTOCOL.md. These exact bytes
// are also asserted by the firmware's C tests — do not change one side alone.
var specVectors = []struct {
	name  string
	frame []byte
}{
	{"PING 0x12345678", []byte{0xAA, 0x55, 0x01, 0x04, 0x78, 0x56, 0x34, 0x12, 0xAE}},
	{"GET_STATUS", []byte{0xAA, 0x55, 0x02, 0x00, 0x2A}},
	{"MOVE 5 -5", []byte{0xAA, 0x55, 0x10, 0x04, 0x05, 0x00, 0xFB, 0xFF, 0x2F}},
	{"RELEASE_ALL", []byte{0xAA, 0x55, 0x15, 0x00, 0x16}},
	{"HELLO v1 fw1.0.0 caps 3", []byte{0xAA, 0x55, 0x81, 0x06, 0x01, 0x01, 0x00, 0x00, 0x03, 0x00, 0x14}},
}

func TestEncodeMatchesSpecVectors(t *testing.T) {
	encoded := map[string][]byte{
		"PING 0x12345678":         EncodePing(0x12345678),
		"GET_STATUS":              EncodeGetStatus(),
		"MOVE 5 -5":               EncodeMove(5, -5),
		"RELEASE_ALL":             EncodeReleaseAll(),
		"HELLO v1 fw1.0.0 caps 3": Encode(TypeHello, []byte{0x01, 0x01, 0x00, 0x00, 0x03, 0x00}),
	}
	for _, vec := range specVectors {
		if got := encoded[vec.name]; !bytes.Equal(got, vec.frame) {
			t.Errorf("%s: encoded % X, spec % X", vec.name, got, vec.frame)
		}
	}
}

func TestDecoderAcceptsSpecVectors(t *testing.T) {
	for _, vec := range specVectors {
		var d Decoder
		var got *Frame
		for _, b := range vec.frame {
			if f, ok := d.Feed(b); ok {
				frameCopy := f
				got = &frameCopy
			}
		}
		if got == nil {
			t.Fatalf("%s: no frame decoded", vec.name)
		}
		if got.Type != vec.frame[2] {
			t.Errorf("%s: type 0x%02X, want 0x%02X", vec.name, got.Type, vec.frame[2])
		}
		if !bytes.Equal(got.Payload, vec.frame[4:len(vec.frame)-1]) {
			t.Errorf("%s: payload % X, want % X", vec.name, got.Payload, vec.frame[4:len(vec.frame)-1])
		}
	}
}

func TestRoundTripAllMessages(t *testing.T) {
	frames := [][]byte{
		EncodePing(0xDEADBEEF),
		EncodeGetStatus(),
		EncodeMove(-32768, 32767),
		EncodeButtons(ButtonLeft | ButtonMiddle),
		EncodeWheel(-1, 127),
		EncodeKeyDown(0xE0),
		EncodeKeyUp(0x04),
		EncodeReleaseAll(),
		EncodeClearBonds(),
	}
	var d Decoder
	var decoded []Frame
	for _, frame := range frames {
		for _, b := range frame {
			if f, ok := d.Feed(b); ok {
				decoded = append(decoded, f)
			}
		}
	}
	if len(decoded) != len(frames) {
		t.Fatalf("decoded %d frames, want %d", len(decoded), len(frames))
	}
	// Spot-check the extreme MOVE survives the round trip.
	move := decoded[2]
	if move.Type != TypeMove || !bytes.Equal(move.Payload, []byte{0x00, 0x80, 0xFF, 0x7F}) {
		t.Errorf("MOVE round trip: type 0x%02X payload % X", move.Type, move.Payload)
	}
}

func TestDecoderRejectsCorruptCRC(t *testing.T) {
	frame := EncodeMove(10, 20)
	frame[len(frame)-1] ^= 0xFF

	var d Decoder
	for _, b := range frame {
		if _, ok := d.Feed(b); ok {
			t.Fatal("corrupt frame was accepted")
		}
	}
	if d.Errors[DecodeErrBadCRC] != 1 {
		t.Errorf("bad CRC count = %d, want 1", d.Errors[DecodeErrBadCRC])
	}
}

func TestDecoderRejectsOversizedLen(t *testing.T) {
	var d Decoder
	for _, b := range []byte{0xAA, 0x55, 0x10, MaxPayload + 1} {
		if _, ok := d.Feed(b); ok {
			t.Fatal("oversized frame was accepted")
		}
	}
	if d.Errors[DecodeErrBadLen] != 1 {
		t.Errorf("bad len count = %d, want 1", d.Errors[DecodeErrBadLen])
	}
}

func TestDecoderResyncsAfterGarbage(t *testing.T) {
	var stream []byte
	stream = append(stream, 0x00, 0xFF, 0xAA, 0x12)             // noise, false sync start
	stream = append(stream, EncodePing(1)...)                   // valid
	stream = append(stream, 0xAA, 0x55, 0x10, 0x04, 0x01, 0x02) // truncated frame...
	stream = append(stream, EncodeGetStatus()...)               // ...whose tail swallows into it
	stream = append(stream, EncodeReleaseAll()...)              // valid again

	var d Decoder
	var decoded []Frame
	for _, b := range stream {
		if f, ok := d.Feed(b); ok {
			decoded = append(decoded, f)
		}
	}
	// The truncated frame eats the GET_STATUS bytes as its payload and dies
	// on CRC; RELEASE_ALL must decode cleanly after resync.
	if len(decoded) != 2 {
		t.Fatalf("decoded %d frames, want 2 (PING + RELEASE_ALL)", len(decoded))
	}
	if decoded[0].Type != TypePing || decoded[1].Type != TypeReleaseAll {
		t.Errorf("decoded types 0x%02X 0x%02X, want PING, RELEASE_ALL", decoded[0].Type, decoded[1].Type)
	}
	if d.Errors[DecodeErrBadCRC] == 0 {
		t.Error("expected a bad CRC from the truncated frame")
	}
}

func TestDecoderHandlesSyncRun(t *testing.T) {
	// A run of 0xAA bytes before a real frame: the last 0xAA is the true sync.
	stream := append([]byte{0xAA, 0xAA, 0xAA}, EncodeGetStatus()...)
	var d Decoder
	var decoded int
	for _, b := range stream {
		if _, ok := d.Feed(b); ok {
			decoded++
		}
	}
	if decoded != 1 {
		t.Errorf("decoded %d frames, want 1", decoded)
	}
}

func TestDecoderChunkedDelivery(t *testing.T) {
	// Byte-at-a-time vs random chunk sizes must be equivalent; Feed is
	// per-byte so we simulate chunking by feeding slices of a long stream.
	var stream []byte
	for i := 0; i < 50; i++ {
		stream = append(stream, EncodeMove(int16(i), int16(-i))...)
		stream = append(stream, EncodePing(uint32(i))...)
	}

	rng := rand.New(rand.NewSource(42))
	var d Decoder
	decoded := 0
	for offset := 0; offset < len(stream); {
		chunk := 1 + rng.Intn(7)
		if offset+chunk > len(stream) {
			chunk = len(stream) - offset
		}
		for _, b := range stream[offset : offset+chunk] {
			if _, ok := d.Feed(b); ok {
				decoded++
			}
		}
		offset += chunk
	}
	if decoded != 100 {
		t.Errorf("decoded %d frames, want 100", decoded)
	}
	if len(d.Errors) != 0 {
		t.Errorf("unexpected decode errors: %v", d.Errors)
	}
}

func TestParseHello(t *testing.T) {
	f := Frame{Type: TypeHello, Payload: []byte{0x01, 0x02, 0x03, 0x04, 0x03, 0x00}}
	h, err := ParseHello(f)
	if err != nil {
		t.Fatal(err)
	}
	if h.ProtoVersion != 1 || h.FwMajor != 2 || h.FwMinor != 3 || h.FwPatch != 4 || h.Caps != 3 {
		t.Errorf("parsed %+v", h)
	}
	if _, err := ParseHello(Frame{Type: TypePong}); err == nil {
		t.Error("ParseHello accepted a PONG frame")
	}
}

func TestParseBleState(t *testing.T) {
	f := Frame{Type: TypeBleState, Payload: []byte{BleConnected, 0x13, 2}}
	s, err := ParseBleState(f)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != BleConnected || s.Reason != 0x13 || s.BondCount != 2 {
		t.Errorf("parsed %+v", s)
	}
}

func TestParsePong(t *testing.T) {
	frame := EncodePing(0xCAFEBABE)
	// PONG carries the same nonce layout; reuse the payload.
	f := Frame{Type: TypePong, Payload: frame[4 : len(frame)-1]}
	nonce, err := ParsePong(f)
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 0xCAFEBABE {
		t.Errorf("nonce = 0x%08X", nonce)
	}
}

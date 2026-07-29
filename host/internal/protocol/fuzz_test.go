package protocol

import (
	"bytes"
	"testing"
)

// FuzzDecoderFeed asserts two invariants over arbitrary byte streams:
// the decoder never panics or reads out of bounds, and any frame it emits
// re-encodes to a byte string that appears in the input stream.
func FuzzDecoderFeed(f *testing.F) {
	f.Add([]byte{})
	f.Add(EncodePing(42))
	f.Add(append([]byte{0xAA, 0xAA, 0x55}, EncodeReleaseAll()...))
	corrupt := EncodeMove(100, -100)
	corrupt[5] ^= 0x01
	f.Add(corrupt)

	f.Fuzz(func(t *testing.T, stream []byte) {
		var d Decoder
		for _, b := range stream {
			frame, ok := d.Feed(b)
			if !ok {
				continue
			}
			if len(frame.Payload) > MaxPayload {
				t.Fatalf("emitted payload of %d bytes", len(frame.Payload))
			}
			if !bytes.Contains(stream, Encode(frame.Type, frame.Payload)) {
				t.Fatalf("emitted frame (type 0x%02X, payload % X) not present in input", frame.Type, frame.Payload)
			}
		}
	})
}

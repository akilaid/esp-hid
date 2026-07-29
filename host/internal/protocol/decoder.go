package protocol

// DecodeError identifies why the decoder rejected bytes.
type DecodeError int

const (
	// DecodeErrBadLen: frame declared a payload longer than MaxPayload.
	DecodeErrBadLen DecodeError = iota
	// DecodeErrBadCRC: frame checksum mismatch.
	DecodeErrBadCRC
)

func (e DecodeError) String() string {
	switch e {
	case DecodeErrBadLen:
		return "bad length"
	case DecodeErrBadCRC:
		return "bad crc"
	default:
		return "unknown"
	}
}

type decoderState int

const (
	stateWaitAA decoderState = iota
	stateWait55
	stateType
	stateLen
	statePayload
	stateCRC
)

// Decoder is an incremental frame decoder. Feed it bytes as they arrive;
// it emits complete valid frames and reports desync errors. The zero value
// is ready to use.
type Decoder struct {
	state   decoderState
	ftype   byte
	length  int
	payload [MaxPayload]byte
	got     int

	// Errors counts rejected frames by cause, for diagnostics.
	Errors map[DecodeError]int
}

// Feed consumes one byte. It returns (frame, true) when the byte completes a
// valid frame; otherwise (Frame{}, false). Rejections are tallied in Errors
// and the decoder resyncs on the next 0xAA 0x55.
func (d *Decoder) Feed(b byte) (Frame, bool) {
	switch d.state {
	case stateWaitAA:
		if b == SyncByte1 {
			d.state = stateWait55
		}
	case stateWait55:
		switch b {
		case SyncByte2:
			d.state = stateType
		case SyncByte1:
			// A run of 0xAA bytes: the last one may still start a frame.
		default:
			d.state = stateWaitAA
		}
	case stateType:
		d.ftype = b
		d.state = stateLen
	case stateLen:
		if int(b) > MaxPayload {
			d.fail(DecodeErrBadLen)
			break
		}
		d.length = int(b)
		d.got = 0
		if d.length == 0 {
			d.state = stateCRC
		} else {
			d.state = statePayload
		}
	case statePayload:
		d.payload[d.got] = b
		d.got++
		if d.got == d.length {
			d.state = stateCRC
		}
	case stateCRC:
		crcInput := make([]byte, 0, 2+d.length)
		crcInput = append(crcInput, d.ftype, byte(d.length))
		crcInput = append(crcInput, d.payload[:d.length]...)
		if CRC8(crcInput) != b {
			d.fail(DecodeErrBadCRC)
			break
		}
		frame := Frame{Type: d.ftype, Payload: append([]byte(nil), d.payload[:d.length]...)}
		d.state = stateWaitAA
		return frame, true
	}
	return Frame{}, false
}

func (d *Decoder) fail(cause DecodeError) {
	if d.Errors == nil {
		d.Errors = make(map[DecodeError]int)
	}
	d.Errors[cause]++
	d.state = stateWaitAA
}

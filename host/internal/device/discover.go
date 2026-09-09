package device

import (
	"errors"
	"sort"
	"strings"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"esp-hid/host/internal/protocol"
)

// Any Espressif chip with a native USB peripheral enumerates as 303A:1001 with
// the same product string, so VID/PID alone cannot tell one board from another,
// and nothing here assumes a particular chip. What does distinguish them is the
// USB serial number: the ROM descriptor reports the chip's factory MAC, unique
// per unit and stable for the life of the board.
//
// The /dev/cu.usbmodemNNNN name is NOT a substitute. It is derived from the USB
// location — which hub port the board is in — so it changes the moment the user
// replugs. The serial is what gets persisted; the port name is looked up fresh
// every time.
var (
	// errNoCandidates: no Espressif native-USB board is attached at all.
	errNoCandidates = errors.New("no ESP32 found (USB 303A:1001)")
	// errNeedsDiscovery: boards are attached but none is bound yet, so which
	// one is the bridge has to be established by handshake.
	errNeedsDiscovery = errors.New("no bridge bound yet")
	// errBoundDeviceAbsent: the bound board is not attached. Deliberately NOT
	// recoverable by substitution — see selectPort.
	errBoundDeviceAbsent = errors.New("bound bridge not connected")
	// errNoBridgeFound: every candidate was probed and none answered.
	errNoBridgeFound = errors.New("no bridge answered on any ESP32 port")
	// errAmbiguousBridge: more than one board answered, so binding one of them
	// would be a guess.
	errAmbiguousBridge = errors.New("more than one bridge answered")
	// errAllCandidatesProbed: everything attached has already been probed once
	// this run and none of it was the bridge. Probing again would just be
	// writing to other people's boards on a 750ms loop.
	errAllCandidatesProbed = errors.New("no unprobed ESP32 ports remain")
)

const (
	// The probe never touches 1200 baud: a 1200-baud open is the bootloader
	// entry "touch" on ESP32-S2/S3 Arduino cores, and this is the one place
	// the app opens a port it has not yet identified.
	probeBaudRate = 115200
	// Per-read timeout while waiting for the reply.
	probeReadTimeout = 100 * time.Millisecond
	// Total time to wait for a HELLO before giving up on a candidate. The
	// device answers GET_STATUS straight from its dispatch loop with no BLE
	// round trip, so this is generous by two orders of magnitude — but not so
	// generous that a board still bringing up NimBLE gets written off.
	probeTimeout = 400 * time.Millisecond
	// How long to wait for open() itself. Normally microseconds; a board whose
	// CDC endpoint is wedged never returns at all, so this is the only thing
	// standing between discovery and a permanently hung reconnect loop.
	probeOpenTimeout = 1500 * time.Millisecond
	// Let the tty finish setup before writing; see probe.
	probeSettle = 20 * time.Millisecond
	// Let the driver finish tearing the fd down before session() reopens.
	probeCloseWait = 30 * time.Millisecond

	// Hard ceiling on GET_STATUS writes for the life of the process. The
	// reasoning above already bounds probing to once per board, which makes
	// this redundant — and that is exactly why it is here. This is the only
	// code that writes to hardware the user has not chosen, so it gets a limit
	// that holds even if the logic guarding it is wrong.
	maxProbeWrites = 16
)

// candidate is one attached Espressif native-USB board.
type candidate struct {
	Port   string // e.g. /dev/cu.usbmodem3301 — positional, not stable
	Serial string // as the OS reports it, for display
	Key    string // normalized, for matching and persistence
	// Hello is set once the board has answered a probe.
	Hello protocol.Hello
}

// normalizeSerial reduces a USB serial string to a key that means the same
// thing on every OS.
//
// macOS reports the board's factory MAC verbatim: "AA:BB:CC:00:00:33". Windows
// parses the serial out of the SetupAPI device instance ID with a `(\w+)$`
// pattern (go.bug.st/serial enumerator/usb_windows.go), and `\w` excludes
// colons — so the same board surfaces there as "AABBCC000033". Folding both to
// uppercase alphanumerics means a settings file written on one OS still matches
// the same physical board on the other.
func normalizeSerial(serialNumber string) string {
	var b strings.Builder
	for _, r := range serialNumber {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		}
	}
	return b.String()
}

// cacheKey identifies a candidate for the probe cache. Boards whose serial the
// OS did not report share the empty key, so they fall back to the port name —
// otherwise two such boards would look like one and only the first would ever
// be probed.
func (c candidate) cacheKey() string {
	if c.Key != "" {
		return c.Key
	}
	return "port:" + c.Port
}

// Label renders a candidate for the user: the serial is the identity, the port
// is the incidental detail, so the serial leads.
func (c candidate) Label() string {
	if c.Serial == "" {
		return c.Port
	}
	return c.Serial + " · " + shortPortName(c.Port)
}

// describeCandidates lists attached boards for a status line. Naming them by
// serial is what lets the user tell which of their ESP32s the app is talking
// about — the port names are interchangeable-looking and change on replug.
func describeCandidates(candidates []candidate) string {
	if len(candidates) == 0 {
		return "none"
	}
	labels := make([]string, 0, len(candidates))
	for _, c := range candidates {
		labels = append(labels, c.Label())
	}
	return strings.Join(labels, ", ")
}

func shortPortName(port string) string {
	if i := strings.LastIndexByte(port, '/'); i >= 0 {
		return port[i+1:]
	}
	return port
}

// selectPort decides which attached board to open. It performs no I/O, so
// every branch below is unit-testable without hardware.
//
// The returned candidate slice is always the full sorted candidate list, even
// on error, so callers can report what *is* attached.
//
// The critical rule is the last one: when the bound board is absent, this
// returns errBoundDeviceAbsent and never falls back to another board — not even
// when exactly one other board is attached. Substituting would mean opening
// somebody else's ESP32 and writing HID traffic at it.
func selectPort(ports []*enumerator.PortDetails, boundKey string) (*candidate, []candidate, error) {
	var all []candidate
	for _, port := range ports {
		if port == nil || !port.IsUSB {
			continue
		}
		if !strings.EqualFold(port.VID, EspressifVID) || !strings.EqualFold(port.PID, UsbJtagPID) {
			continue
		}
		all = append(all, candidate{
			Port:   port.Name,
			Serial: port.SerialNumber,
			Key:    normalizeSerial(port.SerialNumber),
		})
	}
	// Deterministic order, so probing and reporting are reproducible run to
	// run. Enumeration order is IOKit registry order on macOS, which is not.
	sort.Slice(all, func(i, j int) bool {
		if all[i].Key != all[j].Key {
			return all[i].Key < all[j].Key
		}
		return all[i].Port < all[j].Port
	})

	if len(all) == 0 {
		return nil, nil, errNoCandidates
	}
	if boundKey == "" {
		return nil, all, errNeedsDiscovery
	}
	for i := range all {
		// An empty key can never match: a board whose serial the OS withheld
		// is not identifiable, so it must not satisfy a binding.
		if all[i].Key != "" && all[i].Key == boundKey {
			return &all[i], all, nil
		}
	}
	return nil, all, errBoundDeviceAbsent
}

// probeVerdict says what a probe established. The distinction that matters is
// whether anything was actually written: only a board that has been written to
// needs to be remembered as "already touched".
type probeVerdict int

const (
	// verdictHello: a valid HELLO came back. This board is the bridge.
	verdictHello probeVerdict = iota
	// verdictSilent: opened and written to, but nothing valid came back.
	verdictSilent
	// verdictUnavailable: the port refused to open, so NOTHING was written.
	// Retrying costs the board nothing, so this is never cached.
	verdictUnavailable
	// verdictStuck: open() never returned. Nothing was written, but unlike
	// verdictUnavailable this IS cached — see openWithTimeout.
	verdictStuck
)

// openWithTimeout opens a port, giving up if the kernel does not come back.
//
// This is not defensive programming, it is a measured failure: one of the
// ESP32s on the development machine wedges open() indefinitely, and a bare
// os.open with O_NONBLOCK hangs on it just the same, so the block is in the
// USB CDC driver and no open flag avoids it. Without this the very first
// discovery sweep would hang the reconnect loop forever, and the app would
// simply never start — because of a board the user never asked us to touch.
//
// A stuck open cannot be cancelled: the goroutine stays parked in the syscall
// for the life of the process. That is why the caller caches this outcome and
// never retries the port, and it is bounded anyway by the candidate count.
// It returns the open port, or the verdict explaining why there isn't one.
func openWithTimeout(portName string, timeout time.Duration) (serial.Port, probeVerdict) {
	type opened struct {
		port serial.Port
		err  error
	}
	// Buffered so the goroutine can always finish, even after we stop waiting.
	done := make(chan opened, 1)
	go func() {
		port, err := serial.Open(portName, &serial.Mode{BaudRate: probeBaudRate})
		done <- opened{port, err}
	}()

	select {
	case result := <-done:
		if result.err != nil {
			// A refused open — busy, denied, unplugged since enumeration.
			// Nothing was written, so this is retried later, for free.
			return nil, verdictUnavailable
		}
		return result.port, verdictHello // caller continues the handshake

	case <-time.After(timeout):
		// If the open ever completes, hand the port straight back so the
		// descriptor is not leaked along with the goroutine.
		go func() {
			if late := <-done; late.port != nil {
				late.port.Close()
			}
		}()
		return nil, verdictStuck
	}
}

// probe asks one unidentified board whether it is the bridge.
//
// It writes exactly one frame — GET_STATUS, 5 bytes, `AA 55 02 00 2A` — and
// nothing else. Never RELEASE_ALL, never PING: those are for a port already
// known to be ours. The frame contains no 0x0A and no 0x0D, so a board sitting
// at a line-oriented serial console cannot be made to execute anything by it.
//
// A port that will not open — EBUSY because idf.py monitor or the Arduino IDE
// holds it, a permissions error, or the board unplugged since enumeration — is
// a skip, not a failure. Treating somebody else's open serial monitor as a
// fault here would be wrong, and caching it would be worse: if the monitor
// happens to be on the bridge itself, discovery would give up permanently.
func probe(portName string) (protocol.Hello, probeVerdict) {
	// openWithTimeout, not serial.Open: a foreign board can wedge open()
	// forever. See its comment.
	//
	// Note also the Mode deliberately carries no InitialStatusBits. Setting it
	// looks like cheap hardening — hold DTR and RTS low on a board we have not
	// identified — but it makes the library issue a TIOCMSET ioctl during
	// open, and on an Espressif USB CDC endpoint that ioctl never returns:
	// measured here, it hung the app on the first probe. Nothing is lost by
	// omitting it, because those lines are not wired to anything that resets a
	// native-USB Espressif board; boards that do reset on DTR use CP210x/CH340
	// bridge chips, which carry different VID/PIDs and never reach this code.
	port, verdict := openWithTimeout(portName, probeOpenTimeout)
	if port == nil {
		return protocol.Hello{}, verdict
	}
	defer func() {
		port.Close()
		// The driver's teardown can lag the Close, and on success session()
		// reopens this very port moments later. Without this pause that reopen
		// can spuriously fail as busy.
		time.Sleep(probeCloseWait)
	}()

	if err := port.SetReadTimeout(probeReadTimeout); err != nil {
		return protocol.Hello{}, verdictUnavailable
	}
	// A foreign board mid-boot may already have filled the buffer with log
	// output. The decoder resyncs on AA 55 regardless, but starting clean keeps
	// a long boot banner from crowding out the reply inside the timeout.
	_ = port.ResetInputBuffer()
	// serial.Open applies the mode last on darwin; a write issued inside that
	// window can be swallowed, which would read as "this board is not the
	// bridge" and send discovery past the very device it was looking for.
	time.Sleep(probeSettle)

	if _, err := port.Write(protocol.EncodeGetStatus()); err != nil {
		// The open succeeded, so assume the bytes may have reached the wire.
		return protocol.Hello{}, verdictSilent
	}

	var decoder protocol.Decoder
	buf := make([]byte, readBufferSize)
	deadline := time.Now().Add(probeTimeout)
	for time.Now().Before(deadline) {
		// go.bug.st signals a read timeout as (0, nil) on unix — not EOF — so
		// a quiet board simply loops here until the deadline.
		n, err := port.Read(buf)
		if err != nil {
			return protocol.Hello{}, verdictSilent
		}
		for _, b := range buf[:n] {
			frame, ok := decoder.Feed(b)
			if !ok {
				continue
			}
			// The device replies HELLO then BLE_STATE; HELLO is the one that
			// proves it speaks this protocol.
			if frame.Type == protocol.TypeHello {
				if hello, err := protocol.ParseHello(frame); err == nil {
					return hello, verdictHello
				}
			}
		}
	}
	return protocol.Hello{}, verdictSilent
}

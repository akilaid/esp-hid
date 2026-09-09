// Package device manages the serial link to the bridge: discovery by USB
// VID/PID, connect/reconnect, frame IO, and keepalive.
package device

import (
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"esp-hid/host/internal/protocol"
)

// The Espressif native USB Serial/JTAG interface. Fixed in silicon — and the
// same on every Espressif chip that has one, so this pair narrows the field but
// does not identify a board. The USB serial number does; see discover.go.
const (
	EspressifVID = "303A"
	UsbJtagPID   = "1001"
)

const (
	queueDepth        = 1024
	reconnectDelay    = 750 * time.Millisecond
	pingInterval      = time.Second
	maxMissedPongs    = 3
	readBufferSize    = 256
)

// EventKind discriminates link events.
type EventKind int

const (
	EventConnected    EventKind = iota // serial opened; Port is set
	EventDisconnected                  // serial lost; Detail explains why
	EventHello                         // device identity received
	EventBleState                      // device BLE state changed
	EventDeviceError                   // device reported a protocol error
	EventLog                           // device LOG frame (dev firmware builds)
	EventDiscovering                   // probing attached boards for the bridge
	EventDeviceLearned                 // bridge identified; Serial must be persisted
	EventDeviceAbsent                  // the bound bridge is not attached
	EventDeviceAmbiguous               // several boards answered; none bound
)

// Event is one link event. Only the fields relevant to Kind are set.
type Event struct {
	Kind     EventKind
	Port     string
	Detail   string
	Hello    protocol.Hello
	BleState protocol.BleState
	ErrCode  byte
	ErrData  byte
	// Serial is the bridge's USB serial number, set on EventDeviceLearned and
	// EventConnected. On EventDeviceLearned it is the value the caller must
	// persist so later runs skip discovery entirely.
	Serial string
}

// Link owns the connection to the bridge device.
type Link struct {
	events chan<- Event
	queue  chan []byte
	closed chan struct{}

	serialUp     atomic.Bool
	bleConnected atomic.Bool
	portOverride string

	// boundKey is the normalized USB serial of the bridge. While it is set,
	// no other board is ever opened. Discovery fills it in, and the caller
	// persists it so later runs never probe at all.
	boundKey string
	// boundSerial is boundKey's display form, for "waiting for ..." messages.
	boundSerial string
	// probed keys every board this process has already written to, so the
	// 750ms reconnect loop cannot re-probe the user's other ESP32s.
	probed map[string]bool
	// answered keys the boards that replied HELLO. Remembering them means
	// ambiguity can resolve without a second handshake, and a bridge whose
	// serial the OS withheld is still reusable for the rest of the run.
	answered map[string]candidate
	// lastSeen is the previous enumeration, so a board that was unplugged and
	// reattached — and may have been reflashed meanwhile — can be reconsidered.
	lastSeen map[string]bool
	// probeWrites is the lifetime GET_STATUS count, capped at maxProbeWrites.
	probeWrites int
	// probeFn is the handshake, injectable so discovery is testable without
	// hardware.
	probeFn func(string) (protocol.Hello, probeVerdict)
	// lastNotice deduplicates the reconnect loop's repeated explanations.
	lastNotice string
}

// New creates a Link that reports events on the given channel. If portOverride
// is non-empty it wins outright and no discovery happens. Otherwise boundSerial
// — a USB serial number learned by a previous run — pins the board; when it is
// empty the first Run identifies the bridge by handshake and reports it as
// EventDeviceLearned.
func New(events chan<- Event, portOverride, boundSerial string) *Link {
	return &Link{
		events:       events,
		queue:        make(chan []byte, queueDepth),
		closed:       make(chan struct{}),
		portOverride: portOverride,
		boundKey:     normalizeSerial(boundSerial),
		boundSerial:  boundSerial,
		probed:       make(map[string]bool),
		answered:     make(map[string]candidate),
		lastSeen:     make(map[string]bool),
		probeFn:      probe,
	}
}

// Run drives the connect/reconnect loop until Close is called.
func (l *Link) Run() {
	for {
		select {
		case <-l.closed:
			return
		default:
		}
		// resolvePort reports its own reasons — it is the only layer that can
		// tell "nothing attached" from "the bound board is missing".
		portName, err := l.resolvePort()
		if err != nil {
			if !l.sleep(reconnectDelay) {
				return
			}
			continue
		}
		l.lastNotice = ""
		l.session(portName)
		if !l.sleep(reconnectDelay) {
			return
		}
	}
}

// Close stops the link permanently.
func (l *Link) Close() {
	close(l.closed)
}

// SerialUp reports whether the serial link is currently open.
func (l *Link) SerialUp() bool { return l.serialUp.Load() }

// BleConnected reports whether the device says a BLE host is connected.
func (l *Link) BleConnected() bool { return l.bleConnected.Load() }

// QueueUtilization returns the send-queue fill percentage (0-100).
func (l *Link) QueueUtilization() int {
	return len(l.queue) * 100 / cap(l.queue)
}

// EnqueueMove queues a frame that may be silently dropped under congestion
// (movement deltas: the next one corrects for a lost one).
func (l *Link) EnqueueMove(frame []byte) {
	select {
	case l.queue <- frame:
	default:
		// Full: drop the movement frame.
	}
}

// Enqueue queues a frame that must not be lost (clicks, keys, releases).
// When the queue is full the oldest entry is evicted to make room.
func (l *Link) Enqueue(frame []byte) {
	for {
		select {
		case l.queue <- frame:
			return
		default:
			select {
			case <-l.queue:
			default:
			}
		}
	}
}

func (l *Link) emit(event Event) {
	select {
	case l.events <- event:
	case <-l.closed:
	}
}

func (l *Link) sleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-l.closed:
		return false
	}
}

// resolvePort decides which port to open this iteration, and reports why when
// it declines to open anything. It emits its own notices — Run stays quiet —
// because only this function knows whether "no device" means nothing is
// attached, the bound board is missing, or discovery came up empty.
func (l *Link) resolvePort() (string, error) {
	if l.portOverride != "" {
		// An explicitly named port bypasses identification entirely: the user
		// has said which one, so nothing is probed and nothing else is opened.
		return l.portOverride, nil
	}

	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		wrapped := fmt.Errorf("port enumeration failed: %w", err)
		l.notify(Event{Kind: EventDisconnected, Detail: wrapped.Error()})
		return "", wrapped
	}

	chosen, all, err := selectPort(ports, l.boundKey)
	// Every tick, not just the discovery path: when the user unplugs
	// everything the code below returns before discover() is ever reached, and
	// that gesture has to be what clears the cache.
	l.refreshProbeCache(all)
	switch {
	case err == nil:
		return chosen.Port, nil

	case errors.Is(err, errBoundDeviceAbsent):
		// The whole point of the binding: other boards are attached and we
		// will not touch any of them.
		l.notify(Event{
			Kind:   EventDeviceAbsent,
			Serial: l.boundSerial,
			Detail: fmt.Sprintf("waiting for %s (attached: %s)",
				l.boundSerial, describeCandidates(all)),
		})
		return "", err

	case errors.Is(err, errNeedsDiscovery):
		return l.discover(all)

	default:
		l.notify(Event{Kind: EventDisconnected, Detail: err.Error()})
		return "", err
	}
}

// discover identifies the bridge among boards that share its USB VID/PID, by
// asking each one whether it speaks the protocol.
//
// This is the only code path that opens a port the user has not chosen, so it
// is bounded twice over: probe() writes a single GET_STATUS and nothing else,
// and the probed set means each board is handshaked at most once per process
// run. Without that set, Run's 750ms reconnect loop would re-probe the user's
// other ESP32s several times a second for as long as the app is open.
func (l *Link) discover(all []candidate) (string, error) {
	// Boards already proven to be bridges need no second handshake. This is
	// also how ambiguity resolves itself: unplug one of two responders and the
	// survivor binds on the next tick, without anything being probed again.
	if confirmed := l.confirmedPresent(all); len(confirmed) > 0 {
		return l.settle(confirmed)
	}

	fresh := make([]candidate, 0, len(all))
	for _, c := range all {
		if !l.probed[c.cacheKey()] {
			fresh = append(fresh, c)
		}
	}
	if len(fresh) == 0 {
		// Everything attached has been asked once and none of it was the
		// bridge. Asking again would just be writing to other people's boards
		// on a 750ms loop.
		l.notify(Event{Kind: EventDisconnected, Detail: errAllCandidatesProbed.Error()})
		return "", errAllCandidatesProbed
	}

	l.notify(Event{
		Kind:   EventDiscovering,
		Detail: fmt.Sprintf("identifying bridge among %s", describeCandidates(fresh)),
	})

	for _, c := range fresh {
		if l.probeWrites >= maxProbeWrites {
			log.Printf("device: probe budget of %d reached; identifying no further boards",
				maxProbeWrites)
			break
		}
		hello, verdict := l.probeFn(c.Port)
		switch verdict {
		case verdictUnavailable:
			// The port refused to open, so nothing was written. Deliberately
			// not cached: retrying costs that board nothing, and if the busy
			// port happens to be the bridge — someone left idf.py monitor
			// open — caching it here would strand discovery for the whole run.
			continue
		case verdictStuck:
			// open() never returned and a goroutine is parked in the kernel
			// for good. Nothing was written, so this costs no budget, but it
			// is cached: retrying would park another goroutine and still fail.
			log.Printf("device: %s did not respond to open; skipping it for this run", c.Label())
			l.probed[c.cacheKey()] = true
			continue
		}
		// Counted and cached only once a write was attempted, so a board is
		// never written to twice however this loop exits.
		l.probeWrites++
		l.probed[c.cacheKey()] = true
		if verdict == verdictHello {
			c.Hello = hello
			l.answered[c.cacheKey()] = c
		}
	}

	return l.settle(l.confirmedPresent(all))
}

// confirmedPresent lists the boards this run has proven to be bridges and that
// are still attached.
func (l *Link) confirmedPresent(all []candidate) []candidate {
	var found []candidate
	for _, c := range all {
		if known, ok := l.answered[c.cacheKey()]; ok {
			// Take the current port: the board may have moved hubs.
			known.Port = c.Port
			found = append(found, known)
		}
	}
	return found
}

// settle turns the set of confirmed bridges into a decision.
func (l *Link) settle(confirmed []candidate) (string, error) {
	switch len(confirmed) {
	case 0:
		l.notify(Event{Kind: EventDisconnected, Detail: errNoBridgeFound.Error()})
		return "", errNoBridgeFound

	case 1:
		return l.bind(confirmed[0]), nil

	default:
		// Two bridges is a question only the user can answer, so guessing here
		// would reintroduce exactly the bug this change removes.
		l.notify(Event{
			Kind: EventDeviceAmbiguous,
			Detail: fmt.Sprintf("%d bridges answered (%s) — leave one attached",
				len(confirmed), describeCandidates(confirmed)),
		})
		return "", errAmbiguousBridge
	}
}

// bind records the identified bridge and returns the port to open.
func (l *Link) bind(found candidate) string {
	if found.Key == "" {
		// Confirmed as the bridge, but the OS reported no serial, so there is
		// nothing durable to remember it by. Usable for this run only.
		log.Printf("device: bridge on %s reports no USB serial number; "+
			"it cannot be remembered across runs", found.Port)
		return found.Port
	}
	if l.boundKey != found.Key {
		l.boundKey = found.Key
		l.boundSerial = found.Serial
		l.notify(Event{
			Kind:   EventDeviceLearned,
			Serial: found.Serial,
			Port:   found.Port,
			Detail: found.Label(),
		})
	}
	return found.Port
}

// refreshProbeCache decides what the probe cache should forget.
//
// Nothing here is time-based. The cache is invalidated only by the user
// physically changing something, because that is the only thing that can change
// the answer — and because a cache that expired on its own would quietly start
// writing to their other boards again.
func (l *Link) refreshProbeCache(all []candidate) {
	if len(all) == 0 {
		// Everything unplugged. "Unplug them all, then plug in just the
		// bridge" is where any troubleshooting conversation starts, so it has
		// to actually work.
		l.probed = make(map[string]bool)
		l.answered = make(map[string]candidate)
		l.lastSeen = make(map[string]bool)
		return
	}
	present := make(map[string]bool, len(all))
	for _, c := range all {
		key := c.cacheKey()
		present[key] = true
		// Here now, absent a moment ago: it has been unplugged and reattached,
		// so it may have been reflashed since. Let it answer for itself again.
		if l.probed[key] && !l.lastSeen[key] {
			delete(l.probed, key)
			delete(l.answered, key)
		}
	}
	l.lastSeen = present
}

// notify emits an event unless it is identical to the last one emitted.
// resolvePort runs every reconnectDelay, so without this the UI would receive
// the same "waiting for ..." line more than once a second.
func (l *Link) notify(event Event) {
	fingerprint := fmt.Sprintf("%d|%s", event.Kind, event.Detail)
	if fingerprint == l.lastNotice {
		return
	}
	l.lastNotice = fingerprint
	l.emit(event)
}

// session runs one connected session; returns when the link fails or closes.
func (l *Link) session(portName string) {
	port, err := serial.Open(portName, &serial.Mode{BaudRate: 115200})
	if err != nil {
		l.emit(Event{Kind: EventDisconnected, Detail: fmt.Sprintf("open %s: %v", portName, err)})
		return
	}
	defer port.Close()
	_ = port.SetReadTimeout(100 * time.Millisecond)

	l.serialUp.Store(true)
	defer func() {
		l.serialUp.Store(false)
		l.bleConnected.Store(false)
	}()
	l.emit(Event{Kind: EventConnected, Port: portName, Serial: l.boundSerial})

	// Resync: the device does not reset on port open, so ask where it stands,
	// and clear any input state a previous session might have left pressed.
	if _, err := port.Write(protocol.EncodeReleaseAll()); err != nil {
		l.emit(Event{Kind: EventDisconnected, Detail: err.Error()})
		return
	}
	if _, err := port.Write(protocol.EncodeGetStatus()); err != nil {
		l.emit(Event{Kind: EventDisconnected, Detail: err.Error()})
		return
	}

	sessionDead := make(chan struct{})
	writerDone := make(chan struct{})
	pongs := make(chan uint32, 8)

	// Writer + keepalive.
	go func() {
		defer close(writerDone)
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		var nonce uint32
		missed := 0
		for {
			select {
			case <-sessionDead:
				return
			case <-l.closed:
				return
			case frame := <-l.queue:
				if _, err := port.Write(frame); err != nil {
					l.emit(Event{Kind: EventDisconnected, Detail: err.Error()})
					return
				}
			case <-ticker.C:
				// Count pongs since the last ping.
				got := false
				for {
					select {
					case <-pongs:
						got = true
						continue
					default:
					}
					break
				}
				if got {
					missed = 0
				} else {
					missed++
					if missed >= maxMissedPongs {
						l.emit(Event{Kind: EventDisconnected,
							Detail: fmt.Sprintf("%d pings unanswered", missed)})
						return
					}
				}
				nonce++
				if _, err := port.Write(protocol.EncodePing(nonce)); err != nil {
					l.emit(Event{Kind: EventDisconnected, Detail: err.Error()})
					return
				}
			}
		}
	}()

	// Reader.
	var decoder protocol.Decoder
	buf := make([]byte, readBufferSize)
	logLine := ""
	for {
		select {
		case <-l.closed:
			close(sessionDead)
			<-writerDone
			return
		case <-writerDone:
			return
		default:
		}
		n, err := port.Read(buf)
		if err != nil {
			l.emit(Event{Kind: EventDisconnected, Detail: err.Error()})
			close(sessionDead)
			<-writerDone
			return
		}
		for _, b := range buf[:n] {
			frame, ok := decoder.Feed(b)
			if !ok {
				continue
			}
			switch frame.Type {
			case protocol.TypeHello:
				if hello, err := protocol.ParseHello(frame); err == nil {
					l.emit(Event{Kind: EventHello, Hello: hello})
				}
			case protocol.TypeBleState:
				if state, err := protocol.ParseBleState(frame); err == nil {
					l.bleConnected.Store(state.State == protocol.BleConnected)
					l.emit(Event{Kind: EventBleState, BleState: state})
				}
			case protocol.TypePong:
				if nonce, err := protocol.ParsePong(frame); err == nil {
					select {
					case pongs <- nonce:
					default:
					}
				}
			case protocol.TypeAck:
				// Ack of CLEAR_BONDS et al: surface as a log-ish event.
				if len(frame.Payload) > 0 {
					l.emit(Event{Kind: EventLog,
						Detail: fmt.Sprintf("ack 0x%02X", frame.Payload[0])})
				}
			case protocol.TypeError:
				if len(frame.Payload) >= 2 {
					l.emit(Event{Kind: EventDeviceError,
						ErrCode: frame.Payload[0], ErrData: frame.Payload[1]})
				}
			case protocol.TypeLog:
				// Device log lines may span frames; emit on apparent line end.
				logLine += string(frame.Payload)
				if len(frame.Payload) < protocol.MaxPayload {
					l.emit(Event{Kind: EventLog, Detail: logLine})
					logLine = ""
				}
			}
		}
	}
}

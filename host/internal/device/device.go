// Package device manages the serial link to the bridge: discovery by USB
// VID/PID, connect/reconnect, frame IO, and keepalive.
package device

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"esp-hid/host/internal/protocol"
)

// The ESP32-C3 native USB Serial/JTAG interface. Fixed in silicon, so
// discovery needs no port picker and no name heuristics.
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
}

// Link owns the connection to the bridge device.
type Link struct {
	events chan<- Event
	queue  chan []byte
	closed chan struct{}

	serialUp     atomic.Bool
	bleConnected atomic.Bool
	portOverride string
}

// New creates a Link that reports events on the given channel. If
// portOverride is non-empty, that port is used instead of VID/PID discovery.
func New(events chan<- Event, portOverride string) *Link {
	return &Link{
		events:       events,
		queue:        make(chan []byte, queueDepth),
		closed:       make(chan struct{}),
		portOverride: portOverride,
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
		portName, err := l.findPort()
		if err != nil {
			l.emit(Event{Kind: EventDisconnected, Detail: err.Error()})
			if !l.sleep(reconnectDelay) {
				return
			}
			continue
		}
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

func (l *Link) findPort() (string, error) {
	if l.portOverride != "" {
		return l.portOverride, nil
	}
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return "", fmt.Errorf("port enumeration failed: %w", err)
	}
	var candidates []string
	for _, port := range ports {
		if port.IsUSB && strings.EqualFold(port.VID, EspressifVID) &&
			strings.EqualFold(port.PID, UsbJtagPID) {
			candidates = append(candidates, port.Name)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no ESP32-C3 found (USB %s:%s)", EspressifVID, UsbJtagPID)
	}
	if len(candidates) > 1 {
		log.Printf("device: %d ESP32-C3 ports (%s); using %s",
			len(candidates), strings.Join(candidates, ", "), candidates[0])
	}
	return candidates[0], nil
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
	l.emit(Event{Kind: EventConnected, Port: portName})

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

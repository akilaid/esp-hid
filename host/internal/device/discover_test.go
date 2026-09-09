package device

import (
	"errors"
	"testing"

	"go.bug.st/serial/enumerator"

	"esp-hid/host/internal/protocol"
)

// Three stand-in boards. The serials are shaped like the real thing — a
// colon-separated MAC, which is what the ROM descriptor reports — so the
// normalization and ordering tests exercise the format that actually turns up.
const (
	serialA = "AA:BB:CC:00:00:11"
	serialB = "AA:BB:CC:00:00:22"
	serialC = "AA:BB:CC:00:00:33"

	portA = "/dev/cu.usbmodem1101"
	portB = "/dev/cu.usbmodem2201"
	portC = "/dev/cu.usbmodem3301"
)

func espPort(name, serialNumber string) *enumerator.PortDetails {
	return &enumerator.PortDetails{
		Name:         name,
		IsUSB:        true,
		VID:          "303A",
		PID:          "1001",
		SerialNumber: serialNumber,
	}
}

func candidatesOf(ports ...*enumerator.PortDetails) []candidate {
	_, all, _ := selectPort(ports, "")
	return all
}

func TestNormalizeSerial(t *testing.T) {
	cases := []struct{ in, want string }{
		{serialA, "AABBCC000011"},
		{"aa:bb:cc:00:00:11", "AABBCC000011"},
		// Windows reports the same board without separators, because the
		// SetupAPI instance ID grammar cannot carry colons.
		{"AABBCC000011", "AABBCC000011"},
		{"aa-bb-cc 00:00:11", "AABBCC000011"},
		{"", ""},
		{"::--  ", ""},
	}
	for _, c := range cases {
		if got := normalizeSerial(c.in); got != c.want {
			t.Errorf("normalizeSerial(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The macOS and Windows spellings of one board must resolve to one identity,
// or a settings file written on one OS silently stops matching on the other.
func TestBindingSurvivesSerialFormatDifference(t *testing.T) {
	ports := []*enumerator.PortDetails{espPort(portA, serialA)}
	chosen, _, err := selectPort(ports, normalizeSerial("AABBCC000011"))
	if err != nil {
		t.Fatalf("selectPort: %v", err)
	}
	if chosen.Port != portA {
		t.Errorf("chose %q, want %q", chosen.Port, portA)
	}
}

func TestSelectPort(t *testing.T) {
	all3 := []*enumerator.PortDetails{
		espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC),
	}

	tests := []struct {
		name      string
		ports     []*enumerator.PortDetails
		bound     string
		wantErr   error
		wantPort  string
		wantCount int
	}{
		{
			name:    "nothing attached",
			ports:   nil,
			wantErr: errNoCandidates,
		},
		{
			name: "non-Espressif hardware is ignored",
			ports: []*enumerator.PortDetails{
				{Name: "/dev/cu.usbserial", IsUSB: true, VID: "10C4", PID: "EA60"},
				{Name: "/dev/cu.Bluetooth-Incoming-Port", IsUSB: false},
			},
			wantErr: errNoCandidates,
		},
		{
			name:      "unbound with boards attached needs discovery",
			ports:     all3,
			wantErr:   errNeedsDiscovery,
			wantCount: 3,
		},
		{
			name:      "bound board is chosen from among three",
			ports:     all3,
			bound:     normalizeSerial(serialB),
			wantPort:  portB,
			wantCount: 3,
		},
		{
			// The whole point of the change: two other boards are attached and
			// neither is opened.
			name:      "bound board absent with others present",
			ports:     []*enumerator.PortDetails{espPort(portA, serialA), espPort(portC, serialC)},
			bound:     normalizeSerial(serialB),
			wantErr:   errBoundDeviceAbsent,
			wantCount: 2,
		},
		{
			// The dangerous case: a lone board is the most tempting thing to
			// substitute, and must still be refused.
			name:      "bound board absent with exactly one other present",
			ports:     []*enumerator.PortDetails{espPort(portA, serialA)},
			bound:     normalizeSerial(serialB),
			wantErr:   errBoundDeviceAbsent,
			wantCount: 1,
		},
		{
			name:    "bound board absent with nothing attached",
			ports:   nil,
			bound:   normalizeSerial(serialB),
			wantErr: errNoCandidates,
		},
		{
			name:      "lowercase VID/PID still matches",
			ports:     []*enumerator.PortDetails{{Name: portA, IsUSB: true, VID: "303a", PID: "1001", SerialNumber: serialA}},
			wantErr:   errNeedsDiscovery,
			wantCount: 1,
		},
		{
			// The board moved to a different hub port, so its name changed.
			// The binding must not care.
			name:      "bound board at a new port name",
			ports:     []*enumerator.PortDetails{espPort("/dev/cu.usbmodem9999", serialB)},
			bound:     normalizeSerial(serialB),
			wantPort:  "/dev/cu.usbmodem9999",
			wantCount: 1,
		},
		{
			name:      "board without a serial can never satisfy a binding",
			ports:     []*enumerator.PortDetails{espPort(portA, "")},
			bound:     normalizeSerial(serialB),
			wantErr:   errBoundDeviceAbsent,
			wantCount: 1,
		},
		{
			name:      "nil entries are survived",
			ports:     []*enumerator.PortDetails{nil, espPort(portA, serialA)},
			wantErr:   errNeedsDiscovery,
			wantCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chosen, all, err := selectPort(tc.ports, tc.bound)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(all) != tc.wantCount {
				t.Errorf("candidates = %d, want %d", len(all), tc.wantCount)
			}
			if tc.wantPort == "" {
				if chosen != nil {
					t.Errorf("chose %q, want nothing", chosen.Port)
				}
				return
			}
			if chosen == nil {
				t.Fatal("chose nothing, want a port")
			}
			if chosen.Port != tc.wantPort {
				t.Errorf("chose %q, want %q", chosen.Port, tc.wantPort)
			}
		})
	}
}

// Enumeration order is IOKit registry order on macOS, which is not stable.
// Probing and reporting have to be, or a field report is not reproducible.
func TestCandidateOrderIsDeterministic(t *testing.T) {
	forward := candidatesOf(espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC))
	reverse := candidatesOf(espPort(portC, serialC), espPort(portB, serialB), espPort(portA, serialA))
	if len(forward) != len(reverse) {
		t.Fatalf("length mismatch: %d vs %d", len(forward), len(reverse))
	}
	for i := range forward {
		if forward[i].Key != reverse[i].Key {
			t.Fatalf("order differs at %d: %q vs %q", i, forward[i].Key, reverse[i].Key)
		}
	}
	// Sorted by serial, ascending, regardless of the order they enumerated in.
	if forward[0].Serial != serialA || forward[2].Serial != serialC {
		t.Errorf("unexpected order: %v", []string{forward[0].Serial, forward[1].Serial, forward[2].Serial})
	}
}

// fakeProbe stands in for the handshake so discovery can be driven without
// hardware, and — the point of it — so writes to each board can be counted.
type fakeProbe struct {
	bridges map[string]bool // ports that answer HELLO
	busy    map[string]bool // ports that refuse to open
	stuck   map[string]bool // ports whose open() never returns
	calls   map[string]int
}

func newFakeProbe(bridges ...string) *fakeProbe {
	f := &fakeProbe{
		bridges: make(map[string]bool),
		busy:    make(map[string]bool),
		stuck:   make(map[string]bool),
		calls:   make(map[string]int),
	}
	for _, port := range bridges {
		f.bridges[port] = true
	}
	return f
}

func (f *fakeProbe) fn(port string) (protocol.Hello, probeVerdict) {
	f.calls[port]++
	switch {
	case f.stuck[port]:
		return protocol.Hello{}, verdictStuck
	case f.busy[port]:
		return protocol.Hello{}, verdictUnavailable
	case f.bridges[port]:
		return protocol.Hello{ProtoVersion: 1, FwMajor: 1}, verdictHello
	default:
		return protocol.Hello{}, verdictSilent
	}
}

func (f *fakeProbe) total() int {
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func newTestLink(t *testing.T, probeFn func(string) (protocol.Hello, probeVerdict)) *Link {
	t.Helper()
	// Buffered generously: emit blocks, and these tests never drain.
	link := New(make(chan Event, 256), "", "")
	link.probeFn = probeFn
	return link
}

// The safety property this whole change exists to provide, stated as a test:
// the user's other ESP32s are written to once, not once per reconnect tick.
func TestEachBoardIsProbedAtMostOnce(t *testing.T) {
	fake := newFakeProbe() // nothing here is the bridge
	link := newTestLink(t, fake.fn)
	all := candidatesOf(espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC))

	for i := 0; i < 20; i++ {
		link.refreshProbeCache(all)
		if _, err := link.discover(all); err == nil {
			t.Fatalf("iteration %d: discover succeeded unexpectedly", i)
		}
	}

	if fake.total() != 3 {
		t.Errorf("probed %d times across 20 reconnect ticks, want 3 (%v)", fake.total(), fake.calls)
	}
	for _, port := range []string{portA, portB, portC} {
		if fake.calls[port] != 1 {
			t.Errorf("%s probed %d times, want 1", port, fake.calls[port])
		}
	}
	if link.probeWrites != 3 {
		t.Errorf("probeWrites = %d, want 3", link.probeWrites)
	}
}

// A refused open writes nothing, so it must not be remembered — otherwise
// leaving idf.py monitor open on the bridge during first run would strand
// discovery for the rest of the session.
func TestBusyPortIsRetriedAndNeverCached(t *testing.T) {
	fake := newFakeProbe(portB)
	fake.busy[portB] = true
	link := newTestLink(t, fake.fn)
	all := candidatesOf(espPort(portA, serialA), espPort(portB, serialB))

	for i := 0; i < 5; i++ {
		link.refreshProbeCache(all)
		if _, err := link.discover(all); err == nil {
			t.Fatalf("iteration %d: bound a busy board", i)
		}
	}
	if fake.calls[portA] != 1 {
		t.Errorf("silent board probed %d times, want 1", fake.calls[portA])
	}
	if fake.calls[portB] != 5 {
		t.Errorf("busy board probed %d times, want 5 (retried every tick)", fake.calls[portB])
	}
	// A busy port never reaches a write, so it must not consume budget.
	if link.probeWrites != 1 {
		t.Errorf("probeWrites = %d, want 1", link.probeWrites)
	}

	// Once the monitor is closed, the bridge is found without a restart.
	fake.busy[portB] = false
	link.refreshProbeCache(all)
	port, err := link.discover(all)
	if err != nil {
		t.Fatalf("discover after port freed: %v", err)
	}
	if port != portB {
		t.Errorf("bound %q, want %q", port, portB)
	}
}

func TestSingleResponderBindsAndStopsProbing(t *testing.T) {
	fake := newFakeProbe(portC)
	link := newTestLink(t, fake.fn)
	all := candidatesOf(espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC))

	link.refreshProbeCache(all)
	port, err := link.discover(all)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if port != portC {
		t.Errorf("bound port %q, want %q", port, portC)
	}
	if link.boundSerial != serialC {
		t.Errorf("boundSerial = %q, want %q", link.boundSerial, serialC)
	}
	if link.boundKey != normalizeSerial(serialC) {
		t.Errorf("boundKey = %q, want %q", link.boundKey, normalizeSerial(serialC))
	}

	// Once bound, selectPort short-circuits and discovery is never re-entered.
	before := fake.total()
	chosen, _, err := selectPort(
		[]*enumerator.PortDetails{espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC)},
		link.boundKey)
	if err != nil || chosen.Port != portC {
		t.Fatalf("bound lookup: chosen=%v err=%v", chosen, err)
	}
	if fake.total() != before {
		t.Errorf("probing continued after binding")
	}
}

func TestNewBoardIsProbedIncrementally(t *testing.T) {
	fake := newFakeProbe()
	link := newTestLink(t, fake.fn)

	two := candidatesOf(espPort(portA, serialA), espPort(portB, serialB))
	link.refreshProbeCache(two)
	link.discover(two)

	three := candidatesOf(espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC))
	link.refreshProbeCache(three)
	link.discover(three)

	if fake.calls[portA] != 1 || fake.calls[portB] != 1 {
		t.Errorf("already-probed boards were probed again: %v", fake.calls)
	}
	if fake.calls[portC] != 1 {
		t.Errorf("newly attached board probed %d times, want 1", fake.calls[portC])
	}
}

// Unplugging everything is the universal troubleshooting gesture, so it has to
// actually reset discovery.
func TestUnpluggingEverythingClearsTheCache(t *testing.T) {
	fake := newFakeProbe()
	link := newTestLink(t, fake.fn)
	all := candidatesOf(espPort(portA, serialA), espPort(portB, serialB))

	link.refreshProbeCache(all)
	link.discover(all)
	link.refreshProbeCache(nil) // everything unplugged
	link.refreshProbeCache(all)
	link.discover(all)

	if fake.total() != 4 {
		t.Errorf("probed %d times, want 4 (two boards, twice)", fake.total())
	}
}

// A board that was unplugged and came back may have been reflashed, so its old
// "not the bridge" answer is stale. Its neighbours' answers are not.
func TestReplugReprobesOnlyThatBoard(t *testing.T) {
	fake := newFakeProbe()
	link := newTestLink(t, fake.fn)

	all := candidatesOf(espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC))
	link.refreshProbeCache(all)
	link.discover(all)

	withoutB := candidatesOf(espPort(portA, serialA), espPort(portC, serialC))
	link.refreshProbeCache(withoutB)

	link.refreshProbeCache(all)
	link.discover(all)

	if fake.calls[portB] != 2 {
		t.Errorf("replugged board probed %d times, want 2", fake.calls[portB])
	}
	if fake.calls[portA] != 1 || fake.calls[portC] != 1 {
		t.Errorf("untouched boards were re-probed: %v", fake.calls)
	}
}

func TestTwoRespondersBindNothing(t *testing.T) {
	fake := newFakeProbe(portA, portC)
	link := newTestLink(t, fake.fn)
	all := candidatesOf(espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC))

	link.refreshProbeCache(all)
	_, err := link.discover(all)
	if !errors.Is(err, errAmbiguousBridge) {
		t.Fatalf("err = %v, want errAmbiguousBridge", err)
	}
	if link.boundKey != "" {
		t.Errorf("bound %q despite ambiguity", link.boundKey)
	}
}

// Removing one of two bridges resolves the ambiguity without re-probing:
// the survivor already answered once and is remembered.
func TestAmbiguityResolvesWhenOneIsUnplugged(t *testing.T) {
	fake := newFakeProbe(portA, portC)
	link := newTestLink(t, fake.fn)

	both := candidatesOf(espPort(portA, serialA), espPort(portC, serialC))
	link.refreshProbeCache(both)
	if _, err := link.discover(both); !errors.Is(err, errAmbiguousBridge) {
		t.Fatalf("expected ambiguity, got %v", err)
	}
	probesAfterAmbiguity := fake.total()

	onlyA := candidatesOf(espPort(portA, serialA))
	link.refreshProbeCache(onlyA)
	port, err := link.discover(onlyA)
	if err != nil {
		t.Fatalf("discover after unplug: %v", err)
	}
	if port != portA {
		t.Errorf("bound %q, want %q", port, portA)
	}
	if fake.total() != probesAfterAmbiguity {
		t.Errorf("re-probed to resolve ambiguity; the answer was already known")
	}
}

// The backstop. It is redundant with the caching above, which is the point:
// this is the only code that writes to hardware the user did not choose.
func TestProbeWriteBudgetIsEnforced(t *testing.T) {
	fake := newFakeProbe()
	link := newTestLink(t, fake.fn)

	// A fresh board every round, so caching never kicks in.
	for i := 0; i < maxProbeWrites+10; i++ {
		serialNumber := "AA:BB:CC:DD:EE:" + string(rune('A'+i%26)) + string(rune('A'+i/26))
		all := candidatesOf(espPort("/dev/cu.fake", serialNumber))
		link.refreshProbeCache(all)
		link.discover(all)
	}
	if link.probeWrites > maxProbeWrites {
		t.Errorf("probeWrites = %d, exceeds budget of %d", link.probeWrites, maxProbeWrites)
	}
	if fake.total() > maxProbeWrites {
		t.Errorf("probed %d times, exceeds budget of %d", fake.total(), maxProbeWrites)
	}
}

// A board whose open() never returns must be stepped over once and then left
// alone, and it must not stop the real bridge from being found. This is not
// hypothetical: one of the ESP32s on the development machine behaves this way.
func TestStuckBoardIsSkippedOnceAndDoesNotBlockDiscovery(t *testing.T) {
	fake := newFakeProbe(portC)
	fake.stuck[portB] = true
	link := newTestLink(t, fake.fn)
	all := candidatesOf(espPort(portA, serialA), espPort(portB, serialB), espPort(portC, serialC))

	link.refreshProbeCache(all)
	port, err := link.discover(all)
	if err != nil {
		t.Fatalf("a wedged neighbour blocked discovery: %v", err)
	}
	if port != portC {
		t.Errorf("bound %q, want %q", port, portC)
	}

	// And it is never retried: another attempt would park another goroutine
	// in the kernel for the life of the process.
	for i := 0; i < 5; i++ {
		link.refreshProbeCache(all)
		link.discover(all)
	}
	if fake.calls[portB] != 1 {
		t.Errorf("wedged board opened %d times, want 1", fake.calls[portB])
	}
	// A stuck open writes nothing, so it must not consume write budget.
	if link.probeWrites != 2 {
		t.Errorf("probeWrites = %d, want 2 (the two boards that opened)", link.probeWrites)
	}
}

// A bound-but-absent device must never reach the probe at all: no opens, no
// writes, no substitution.
func TestBoundAbsentNeverProbes(t *testing.T) {
	fake := newFakeProbe(portA)
	link := newTestLink(t, fake.fn)
	link.boundKey = normalizeSerial(serialB)
	link.boundSerial = serialB

	ports := []*enumerator.PortDetails{espPort(portA, serialA), espPort(portC, serialC)}
	chosen, all, err := selectPort(ports, link.boundKey)
	if !errors.Is(err, errBoundDeviceAbsent) {
		t.Fatalf("err = %v, want errBoundDeviceAbsent", err)
	}
	if chosen != nil {
		t.Fatalf("chose %q while bound device absent", chosen.Port)
	}
	if len(all) != 2 {
		t.Errorf("candidates = %d, want 2 reported for the status line", len(all))
	}
	if fake.total() != 0 {
		t.Errorf("probed %d times while bound; want 0", fake.total())
	}
}

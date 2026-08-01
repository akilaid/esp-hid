// Package config defines runtime settings with three-layer precedence:
// built-in defaults, persisted settings.json, explicit CLI flags.
package config

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Host placement relative to the slave device. This is the shared vocabulary
// for the -host-side flag, the persisted setting, and the GUI pickers.
const (
	HostSideLeft   = "left"
	HostSideRight  = "right"
	HostSideTop    = "top"
	HostSideBottom = "bottom"
)

// Config is the resolved runtime configuration.
type Config struct {
	PortOverride    string        // empty = auto-discover by USB VID/PID
	MoveRateHz      int           // movement send rate
	MoveDeadzone    int           // per-axis deadzone in pixels
	MoveSmoothing   float64       // micro-smoothing factor [0,1)
	AdaptiveMoves   bool          // backpressure on movement sends
	LeftwardReturn  bool          // left-swipe return gesture
	SlaveWidth      int           // virtual slave resolution
	SlaveHeight     int
	HostSide        string        // left|right|top|bottom
	ReconnectDelay  time.Duration
	CaptureKeyboard bool
	ToggleHotkey    string // e.g. "F9", "Ctrl+Alt+F9"
	AutoSwitch      bool
	GUIMode         bool
	CLIMode         bool // headless diagnostics mode

	// DebugStallCapture deliberately stalls the first captured input event so
	// the OS disables the hook/tap, exercising the recovery path. Diagnostic
	// only, never persisted.
	DebugStallCapture bool
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		MoveRateHz:      45,
		MoveDeadzone:    1,
		MoveSmoothing:   0.2,
		AdaptiveMoves:   true,
		SlaveWidth:      1920,
		SlaveHeight:     1080,
		HostSide:        HostSideLeft,
		ReconnectDelay:  750 * time.Millisecond,
		CaptureKeyboard: true,
		ToggleHotkey:    "F9",
		AutoSwitch:      true,
		GUIMode:         true,
	}
}

// Parse resolves configuration: defaults <- settings.json <- CLI flags.
func Parse(args []string) (Config, error) {
	cfg := Defaults()
	// Persisted settings override defaults before flags are applied.
	if persisted, err := loadSettings(); err == nil {
		persisted.applyTo(&cfg)
	}

	fs := flag.NewFlagSet("esp-hid-bridge", flag.ContinueOnError)
	port := fs.String("port", cfg.PortOverride, "serial port (default: auto-detect by USB VID/PID 303A:1001)")
	rate := fs.Int("rate", cfg.MoveRateHz, "maximum move send rate (events per second)")
	deadzone := fs.Int("deadzone", cfg.MoveDeadzone, "ignore tiny move deltas up to this absolute value (0 disables)")
	smooth := fs.Float64("smooth", cfg.MoveSmoothing, "micro-smoothing factor for small movement (0 disables)")
	adaptive := fs.Bool("adaptive", cfg.AdaptiveMoves, "adapt move send cadence when the link is congested")
	leftReturn := fs.Bool("leftreturn", cfg.LeftwardReturn, "allow returning to host via a deliberate left-swipe")
	slaveRes := fs.String("slave-res", fmt.Sprintf("%dx%d", cfg.SlaveWidth, cfg.SlaveHeight), "virtual slave resolution WIDTHxHEIGHT")
	hostSide := fs.String("host-side", cfg.HostSide, "host placement relative to slave: left|right|top|bottom")
	reconnect := fs.Duration("reconnect", cfg.ReconnectDelay, "reconnect delay after link failure")
	keyboard := fs.Bool("keyboard", cfg.CaptureKeyboard, "capture and forward keyboard events")
	toggle := fs.String("toggle", cfg.ToggleHotkey, "hotkey to toggle remote mode")
	autoSwitch := fs.Bool("auto-switch", cfg.AutoSwitch, "jump to remote device when the cursor hits the screen edge")
	gui := fs.Bool("gui", cfg.GUIMode, "run with GUI")
	cli := fs.Bool("cli", false, "headless diagnostics mode (implies -gui=false)")
	debugStall := fs.Bool("debug-stall-capture", false,
		"diagnostic: stall the first captured event so the OS disables the hook, to verify recovery")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg.PortOverride = *port
	cfg.MoveRateHz = *rate
	cfg.MoveDeadzone = *deadzone
	cfg.MoveSmoothing = *smooth
	cfg.AdaptiveMoves = *adaptive
	cfg.LeftwardReturn = *leftReturn
	cfg.HostSide = strings.ToLower(*hostSide)
	cfg.ReconnectDelay = *reconnect
	cfg.CaptureKeyboard = *keyboard
	cfg.ToggleHotkey = *toggle
	cfg.AutoSwitch = *autoSwitch
	cfg.GUIMode = *gui && !*cli
	cfg.CLIMode = *cli
	cfg.DebugStallCapture = *debugStall

	width, height, err := ParseResolution(*slaveRes)
	if err != nil {
		return Config{}, err
	}
	cfg.SlaveWidth = width
	cfg.SlaveHeight = height

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks value ranges.
func (c Config) Validate() error {
	if c.MoveRateHz <= 0 {
		return fmt.Errorf("rate must be greater than 0")
	}
	if c.MoveDeadzone < 0 {
		return fmt.Errorf("deadzone cannot be negative")
	}
	if c.MoveSmoothing < 0 || c.MoveSmoothing >= 1 {
		return fmt.Errorf("smooth must be in range [0, 1)")
	}
	if c.ReconnectDelay <= 0 {
		return fmt.Errorf("reconnect delay must be greater than 0")
	}
	switch c.HostSide {
	case HostSideLeft, HostSideRight, HostSideTop, HostSideBottom:
	default:
		return fmt.Errorf("invalid host-side %q (supported: left|right|top|bottom)", c.HostSide)
	}
	if c.SlaveWidth < 320 || c.SlaveWidth > 10000 ||
		c.SlaveHeight < 320 || c.SlaveHeight > 10000 {
		return fmt.Errorf("slave resolution out of range (320..10000 per axis)")
	}
	return nil
}

// ParseResolution parses "1920x1080" (case-insensitive, spaces tolerated).
func ParseResolution(s string) (int, int, error) {
	cleaned := strings.ReplaceAll(strings.ToLower(s), " ", "")
	parts := strings.Split(cleaned, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid slave-res %q (use WIDTHxHEIGHT, e.g. 1920x1080)", s)
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid slave-res width %q", parts[0])
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid slave-res height %q", parts[1])
	}
	return width, height, nil
}

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	serial "go.bug.st/serial"
)

type config struct {
	portName         string
	autoPort         bool
	baudRate         int
	moveRateHz       int
	moveDeadzone     int
	moveSmoothing    float64
	adaptiveMoves    bool
	leftwardReturn   bool
	slaveWidth       int
	slaveHeight      int
	hostSide         string
	reconnectDelay   time.Duration
	captureKeyboard  bool
	toggleHotkeyName string
	toggleHotkeyVK   uint32
	toggleHotkeyMods uint32
	autoSwitch       bool
	guiMode          bool
}

const (
	hostSideLeft   = "left"
	hostSideRight  = "right"
	hostSideTop    = "top"
	hostSideBottom = "bottom"

	defaultHostSide        = hostSideLeft
	defaultSlaveResolution = "1920x1080"
	defaultSlaveWidth      = 1920
	defaultSlaveHeight     = 1080
	minSlaveResolutionAxis = 320
	maxSlaveResolutionAxis = 10000
)

var slaveResolutionChoices = []string{
	"1280x720",
	"1280x800",
	"1366x768",
	"1440x900",
	"1536x864",
	"1600x900",
	"1680x1050",
	"1920x1080",
	"1920x1200",
	"2160x1440",
	"2256x1504",
	"2304x1440",
	"2560x1440",
	"2560x1600",
	"2736x1824",
	"2880x1800",
	"3000x2000",
	"3200x1800",
	"720x1280",
	"800x1280",
	"1080x1920",
	"1080x2160",
	"1080x2280",
	"1080x2340",
	"1080x2400",
	"1170x2532",
	"1200x1920",
	"1200x2000",
	"1284x2778",
	"1440x2560",
	"1440x2960",
	"1536x2048",
	"1600x2560",
	"1620x2160",
	"1668x2388",
	"1768x2208",
	"2048x2732",
}

var hostSideChoices = []string{
	hostSideLeft,
	hostSideRight,
	hostSideTop,
	hostSideBottom,
}

func parseSlaveResolution(value string) (int, int, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return 0, 0, false
	}

	normalized = strings.ReplaceAll(normalized, " ", "")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}

	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}

	if width < minSlaveResolutionAxis || width > maxSlaveResolutionAxis {
		return 0, 0, false
	}

	if height < minSlaveResolutionAxis || height > maxSlaveResolutionAxis {
		return 0, 0, false
	}

	return width, height, true
}

func formatSlaveResolution(width int, height int) string {
	return fmt.Sprintf("%dx%d", width, height)
}

func normalizeHostSide(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case hostSideLeft:
		return hostSideLeft, true
	case hostSideRight:
		return hostSideRight, true
	case hostSideTop:
		return hostSideTop, true
	case hostSideBottom:
		return hostSideBottom, true
	default:
		return "", false
	}
}

func defaultConfig() config {
	toggleVK, toggleMods := toggleHotkeyNameToVKMods(defaultToggleHotkeyName)

	return config{
		portName:         "auto",
		autoPort:         true,
		baudRate:         460800,
		moveRateHz:       45,
		moveDeadzone:     1,
		moveSmoothing:    0.2,
		adaptiveMoves:    true,
		leftwardReturn:   false,
		slaveWidth:       defaultSlaveWidth,
		slaveHeight:      defaultSlaveHeight,
		hostSide:         defaultHostSide,
		reconnectDelay:   750 * time.Millisecond,
		captureKeyboard:  true,
		toggleHotkeyName: defaultToggleHotkeyName,
		toggleHotkeyVK:   toggleVK,
		toggleHotkeyMods: toggleMods,
		autoSwitch:       true,
		guiMode:          true,
	}
}

func parseConfig() (config, error) {
	defaultCfg := defaultConfig()
	loadedCfg, loadErr := loadSettingsConfig(defaultCfg)
	if loadErr != nil {
		log.Printf("settings load failed, using built-in defaults: %v", loadErr)
	} else {
		defaultCfg = loadedCfg
	}

	defaultPort := defaultCfg.portName
	if defaultPort == "" || defaultCfg.autoPort {
		defaultPort = "auto"
	}

	defaultReconnectDelay := defaultCfg.reconnectDelay
	if defaultReconnectDelay <= 0 {
		defaultReconnectDelay = 750 * time.Millisecond
	}

	defaultToggle := defaultCfg.toggleHotkeyName
	if normalizedToggle, ok := normalizeToggleHotkeyName(defaultToggle); ok {
		defaultToggle = normalizedToggle
	} else {
		defaultToggle = defaultToggleHotkeyName
	}

	defaultHostSideValue := defaultCfg.hostSide
	if normalizedHostSide, ok := normalizeHostSide(defaultHostSideValue); ok {
		defaultHostSideValue = normalizedHostSide
	} else {
		defaultHostSideValue = defaultHostSide
	}

	defaultSlaveResolutionValue := formatSlaveResolution(defaultCfg.slaveWidth, defaultCfg.slaveHeight)
	if _, _, ok := parseSlaveResolution(defaultSlaveResolutionValue); !ok {
		defaultSlaveResolutionValue = defaultSlaveResolution
	}

	port := flag.String("port", defaultPort, "Serial port connected to the ESP32 (or 'auto')")
	baud := flag.Int("baud", defaultCfg.baudRate, "Serial baud rate")
	rate := flag.Int("rate", defaultCfg.moveRateHz, "Maximum move send rate (events per second)")
	deadzone := flag.Int("deadzone", defaultCfg.moveDeadzone, "Ignore tiny move deltas up to this absolute value (0 disables)")
	smooth := flag.Float64("smooth", defaultCfg.moveSmoothing, "Micro-smoothing factor for small movement (0 disables)")
	adaptive := flag.Bool("adaptive", defaultCfg.adaptiveMoves, "Adapt move send cadence when serial queue is congested")
	leftReturn := flag.Bool("leftreturn", defaultCfg.leftwardReturn, "Allow returning to host via a deliberate left-swipe gesture while in remote mode")
	slaveResolution := flag.String("slave-res", defaultSlaveResolutionValue, "Virtual slave screen resolution WIDTHxHEIGHT")
	hostSide := flag.String("host-side", defaultHostSideValue, "Host placement relative to slave: left|right|top|bottom")
	reconnect := flag.Duration("reconnect", defaultReconnectDelay, "Reconnect delay after serial failure")
	keyboard := flag.Bool("keyboard", defaultCfg.captureKeyboard, "Capture and forward keyboard key down/up events")
	toggle := flag.String("toggle", defaultToggle, "Hotkey to toggle remote mode")
	autoSwitch := flag.Bool("auto-switch", defaultCfg.autoSwitch, "Automatically jump to remote device when mouse moved to edge of screens")
	gui := flag.Bool("gui", defaultCfg.guiMode, "Run with native macOS GUI")
	flag.Parse()

	autoPort := strings.EqualFold(*port, "auto")

	normalizedToggle, ok := normalizeToggleHotkeyName(*toggle)
	if !ok {
		return config{}, fmt.Errorf("invalid toggle hotkey %q", *toggle)
	}
	toggleVK, toggleMods := toggleHotkeyNameToVKMods(normalizedToggle)

	normalizedHostSide, ok := normalizeHostSide(*hostSide)
	if !ok {
		return config{}, fmt.Errorf("invalid host-side %q (supported: left|right|top|bottom)", *hostSide)
	}

	slaveWidth, slaveHeight, ok := parseSlaveResolution(*slaveResolution)
	if !ok {
		return config{}, fmt.Errorf("invalid slave-res %q (use WIDTHxHEIGHT, e.g. 1920x1080)", *slaveResolution)
	}

	cfg := config{
		portName:         *port,
		autoPort:         autoPort,
		baudRate:         *baud,
		moveRateHz:       *rate,
		moveDeadzone:     *deadzone,
		moveSmoothing:    *smooth,
		adaptiveMoves:    *adaptive,
		leftwardReturn:   *leftReturn,
		slaveWidth:       slaveWidth,
		slaveHeight:      slaveHeight,
		hostSide:         normalizedHostSide,
		reconnectDelay:   *reconnect,
		captureKeyboard:  *keyboard,
		toggleHotkeyName: normalizedToggle,
		toggleHotkeyVK:   toggleVK,
		toggleHotkeyMods: toggleMods,
		autoSwitch:       *autoSwitch,
		guiMode:          *gui,
	}

	if cfg.portName == "" && !cfg.autoPort {
		return cfg, errors.New("port cannot be empty")
	}
	if cfg.baudRate <= 0 {
		return cfg, errors.New("baud must be greater than 0")
	}
	if cfg.moveRateHz <= 0 {
		return cfg, errors.New("rate must be greater than 0")
	}
	if cfg.moveDeadzone < 0 {
		return cfg, errors.New("deadzone cannot be negative")
	}
	if cfg.moveSmoothing < 0 || cfg.moveSmoothing >= 1 {
		return cfg, errors.New("smooth must be in range [0, 1)")
	}
	if cfg.reconnectDelay <= 0 {
		return cfg, errors.New("reconnect delay must be greater than 0")
	}
	if cfg.slaveWidth <= 0 || cfg.slaveHeight <= 0 {
		return cfg, errors.New("slave resolution must be greater than 0")
	}

	return cfg, nil
}

func startupPortHint(cfg config) {
	ports, err := listSerialPorts()
	if err != nil {
		log.Printf("unable to list serial ports: %v", err)
		return
	}

	if cfg.autoPort {
		selected, selectErr := autoSelectPort()
		if selectErr != nil {
			log.Printf("serial auto-detect: %v", selectErr)
			return
		}
		log.Printf("serial auto-detect selected %s (available: %s)", selected, portsToString(ports))
		return
	}

	requested := cfg.portName
	found := false
	for _, port := range ports {
		if strings.EqualFold(port, requested) {
			found = true
			break
		}
	}

	if !found {
		log.Printf("warning: requested serial port %s not currently present (available: %s)", cfg.portName, portsToString(ports))
	}
}

// macOSPortPriority returns how likely a port name is an ESP32 USB serial adapter.
// Higher is more likely.
func macOSPortPriority(port string) int {
	lower := strings.ToLower(port)
	switch {
	case strings.Contains(lower, "usbserial"):
		return 4
	case strings.Contains(lower, "slab_usbtouart") || strings.Contains(lower, "slabusbtouart"):
		return 4
	case strings.Contains(lower, "usbmodem"):
		return 3
	case strings.Contains(lower, "wchusbserial"):
		return 3
	case strings.Contains(lower, "ch340") || strings.Contains(lower, "ch341"):
		return 3
	case strings.HasPrefix(lower, "/dev/tty."):
		return 1
	case strings.HasPrefix(lower, "/dev/cu."):
		return 0
	default:
		return -1
	}
}

func listSerialPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}

	sort.Slice(ports, func(i, j int) bool {
		pi := macOSPortPriority(ports[i])
		pj := macOSPortPriority(ports[j])
		if pi != pj {
			return pi > pj
		}
		return ports[i] < ports[j]
	})

	return ports, nil
}

func portsToString(ports []string) string {
	if len(ports) == 0 {
		return "none"
	}
	return strings.Join(ports, ", ")
}

func autoSelectPort() (string, error) {
	ports, err := listSerialPorts()
	if err != nil {
		return "", fmt.Errorf("listing serial ports failed: %w", err)
	}
	if len(ports) == 0 {
		return "", errors.New("no serial ports available")
	}

	// Pick highest-priority port (listSerialPorts sorts by priority descending)
	return ports[0], nil
}

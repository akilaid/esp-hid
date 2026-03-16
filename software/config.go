//go:build windows

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
	// Common laptop and desktop landscape resolutions.
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

	// Common phone and tablet portrait resolutions.
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

func parseConfig() (config, error) {
	port := flag.String("port", "auto", "Serial COM port connected to the ESP32 (or 'auto')")
	baud := flag.Int("baud", 230400, "Serial baud rate")
	rate := flag.Int("rate", 45, "Maximum move send rate (events per second)")
	deadzone := flag.Int("deadzone", 1, "Ignore tiny move deltas up to this absolute value (0 disables)")
	smooth := flag.Float64("smooth", 0.2, "Micro-smoothing factor for small movement (0 disables)")
	adaptive := flag.Bool("adaptive", true, "Adapt move send cadence when serial queue is congested")
	leftReturn := flag.Bool("leftreturn", false, "Allow returning to host via a deliberate left-swipe gesture while in remote mode")
	slaveResolution := flag.String("slave-res", defaultSlaveResolution, "Virtual slave screen resolution WIDTHxHEIGHT used for edge-aware return detection")
	hostSide := flag.String("host-side", defaultHostSide, "Host placement relative to slave: left|right|top|bottom")
	reconnect := flag.Duration("reconnect", 750*time.Millisecond, "Reconnect delay after serial failure")
	keyboard := flag.Bool("keyboard", true, "Capture and forward keyboard key down/up events")
	toggle := flag.String("toggle", defaultToggleHotkeyName, "Hotkey to toggle remote mode (F1-F12)")
	gui := flag.Bool("gui", true, "Run with native Windows GUI")
	flag.Parse()

	autoPort := strings.EqualFold(*port, "auto")

	normalizedToggle, ok := normalizeToggleHotkeyName(*toggle)
	if !ok {
		return config{}, fmt.Errorf("invalid toggle hotkey %q (supported: F1-F12)", *toggle)
	}
	toggleVK, _ := toggleHotkeyNameToVK(normalizedToggle)

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
	if _, ok := normalizeHostSide(cfg.hostSide); !ok {
		return cfg, errors.New("host side must be one of: left, right, top, bottom")
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

	requested := strings.ToUpper(cfg.portName)
	found := false
	for _, port := range ports {
		if strings.ToUpper(port) == requested {
			found = true
			break
		}
	}

	if !found {
		log.Printf("warning: requested serial port %s not currently present (available: %s)", cfg.portName, portsToString(ports))
	}
}

func comPortNumber(port string) (int, bool) {
	name := strings.ToUpper(strings.TrimSpace(port))
	if !strings.HasPrefix(name, "COM") {
		return 0, false
	}

	number, err := strconv.Atoi(strings.TrimPrefix(name, "COM"))
	if err != nil || number <= 0 {
		return 0, false
	}

	return number, true
}

func listSerialPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}

	sort.Slice(ports, func(i, j int) bool {
		leftNumber, leftIsCOM := comPortNumber(ports[i])
		rightNumber, rightIsCOM := comPortNumber(ports[j])

		if leftIsCOM && rightIsCOM {
			return leftNumber < rightNumber
		}
		if leftIsCOM != rightIsCOM {
			return leftIsCOM
		}

		return strings.ToUpper(ports[i]) < strings.ToUpper(ports[j])
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

	selected := ports[0]
	highestComNumber := -1
	for _, port := range ports {
		if number, ok := comPortNumber(port); ok && number > highestComNumber {
			highestComNumber = number
			selected = port
		}
	}

	return selected, nil
}

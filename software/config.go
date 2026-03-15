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
	portName        string
	autoPort        bool
	baudRate        int
	moveRateHz      int
	reconnectDelay  time.Duration
	captureKeyboard bool
	guiMode         bool
}

func parseConfig() (config, error) {
	port := flag.String("port", "auto", "Serial COM port connected to the ESP32 (or 'auto')")
	baud := flag.Int("baud", 115200, "Serial baud rate")
	rate := flag.Int("rate", 60, "Maximum move send rate (events per second)")
	reconnect := flag.Duration("reconnect", 750*time.Millisecond, "Reconnect delay after serial failure")
	keyboard := flag.Bool("keyboard", true, "Capture and forward keyboard key down/up events")
	gui := flag.Bool("gui", true, "Run with native Windows GUI")
	flag.Parse()

	autoPort := strings.EqualFold(*port, "auto")

	cfg := config{
		portName:        *port,
		autoPort:        autoPort,
		baudRate:        *baud,
		moveRateHz:      *rate,
		reconnectDelay:  *reconnect,
		captureKeyboard: *keyboard,
		guiMode:         *gui,
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
	if cfg.reconnectDelay <= 0 {
		return cfg, errors.New("reconnect delay must be greater than 0")
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

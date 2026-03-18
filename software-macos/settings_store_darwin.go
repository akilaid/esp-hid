package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	settingsDirName  = "ESP HID Bridge"
	settingsFileName = "settings.json"
)

type persistedSettings struct {
	Version          int     `json:"version"`
	PortName         string  `json:"portName"`
	AutoPort         bool    `json:"autoPort"`
	BaudRate         int     `json:"baudRate"`
	MoveRateHz       int     `json:"moveRateHz"`
	MoveDeadzone     int     `json:"moveDeadzone"`
	MoveSmoothing    float64 `json:"moveSmoothing"`
	AdaptiveMoves    bool    `json:"adaptiveMoves"`
	LeftwardReturn   bool    `json:"leftwardReturn"`
	SlaveWidth       int     `json:"slaveWidth"`
	SlaveHeight      int     `json:"slaveHeight"`
	HostSide         string  `json:"hostSide"`
	ReconnectDelayMs int     `json:"reconnectDelayMs"`
	CaptureKeyboard  bool    `json:"captureKeyboard"`
	ToggleHotkeyName string  `json:"toggleHotkeyName"`
	ToggleHotkeyMods uint32  `json:"toggleHotkeyMods"`
	AutoSwitch       bool    `json:"autoSwitch"`
	GUIMode          bool    `json:"guiMode"`
}

// settingsFilePath returns the path to the settings file.
// On macOS, os.UserConfigDir() returns ~/Library/Application Support/
func settingsFilePath() (string, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}

	return filepath.Join(userConfigDir, settingsDirName, settingsFileName), nil
}

func loadSettingsConfig(defaults config) (config, error) {
	path, err := settingsFilePath()
	if err != nil {
		return defaults, err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return defaults, fmt.Errorf("read settings file: %w", err)
	}

	var persisted persistedSettings
	if err := json.Unmarshal(contents, &persisted); err != nil {
		return defaults, fmt.Errorf("parse settings json: %w", err)
	}

	cfg := defaults

	portName := strings.TrimSpace(persisted.PortName)
	if persisted.AutoPort || strings.EqualFold(portName, "auto") {
		cfg.portName = "auto"
		cfg.autoPort = true
	} else if portName != "" {
		cfg.portName = portName
		cfg.autoPort = false
	}

	if persisted.BaudRate > 0 {
		cfg.baudRate = persisted.BaudRate
	}
	if persisted.MoveRateHz > 0 {
		cfg.moveRateHz = persisted.MoveRateHz
	}
	if persisted.MoveDeadzone >= 0 {
		cfg.moveDeadzone = persisted.MoveDeadzone
	}
	if persisted.MoveSmoothing >= 0 && persisted.MoveSmoothing < 1 {
		cfg.moveSmoothing = persisted.MoveSmoothing
	}
	if persisted.ReconnectDelayMs > 0 {
		cfg.reconnectDelay = time.Duration(persisted.ReconnectDelayMs) * time.Millisecond
	}

	cfg.adaptiveMoves = persisted.AdaptiveMoves
	cfg.leftwardReturn = persisted.LeftwardReturn
	cfg.captureKeyboard = persisted.CaptureKeyboard
	cfg.autoSwitch = persisted.AutoSwitch
	cfg.guiMode = persisted.GUIMode

	if persisted.SlaveWidth >= minSlaveResolutionAxis &&
		persisted.SlaveWidth <= maxSlaveResolutionAxis &&
		persisted.SlaveHeight >= minSlaveResolutionAxis &&
		persisted.SlaveHeight <= maxSlaveResolutionAxis {
		cfg.slaveWidth = persisted.SlaveWidth
		cfg.slaveHeight = persisted.SlaveHeight
	}

	if normalizedHostSide, ok := normalizeHostSide(persisted.HostSide); ok {
		cfg.hostSide = normalizedHostSide
	}

	if normalizedToggle, ok := normalizeToggleHotkeyName(persisted.ToggleHotkeyName); ok {
		cfg.toggleHotkeyName = normalizedToggle
		vk, mods := toggleHotkeyNameToVKMods(normalizedToggle)
		cfg.toggleHotkeyVK = vk
		cfg.toggleHotkeyMods = mods
	}

	if cfg.autoPort {
		cfg.portName = "auto"
	}

	return cfg, nil
}

func saveSettingsConfig(cfg config) error {
	path, err := settingsFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	persisted := persistedSettings{
		Version:          1,
		PortName:         cfg.portName,
		AutoPort:         cfg.autoPort,
		BaudRate:         cfg.baudRate,
		MoveRateHz:       cfg.moveRateHz,
		MoveDeadzone:     cfg.moveDeadzone,
		MoveSmoothing:    cfg.moveSmoothing,
		AdaptiveMoves:    cfg.adaptiveMoves,
		LeftwardReturn:   cfg.leftwardReturn,
		SlaveWidth:       cfg.slaveWidth,
		SlaveHeight:      cfg.slaveHeight,
		HostSide:         cfg.hostSide,
		ReconnectDelayMs: int(cfg.reconnectDelay / time.Millisecond),
		CaptureKeyboard:  cfg.captureKeyboard,
		ToggleHotkeyName: cfg.toggleHotkeyName,
		ToggleHotkeyMods: cfg.toggleHotkeyMods,
		AutoSwitch:       cfg.autoSwitch,
		GUIMode:          cfg.guiMode,
	}

	if persisted.AutoPort {
		persisted.PortName = "auto"
	}

	encoded, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize settings json: %w", err)
	}

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("write temp settings file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace settings file: %w", err)
	}

	return nil
}

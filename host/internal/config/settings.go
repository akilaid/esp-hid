package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	settingsDirName  = "ESP HID Bridge"
	settingsFileName = "settings-v2.json"
)

// persistedSettings uses pointer fields so absent keys keep their defaults —
// this fixes the legacy bug where a missing boolean silently forced false.
type persistedSettings struct {
	Version         int      `json:"version"`
	PortOverride    *string  `json:"portOverride,omitempty"`
	MoveRateHz      *int     `json:"moveRateHz,omitempty"`
	MoveDeadzone    *int     `json:"moveDeadzone,omitempty"`
	MoveSmoothing   *float64 `json:"moveSmoothing,omitempty"`
	AdaptiveMoves   *bool    `json:"adaptiveMoves,omitempty"`
	LeftwardReturn  *bool    `json:"leftwardReturn,omitempty"`
	SlaveWidth      *int     `json:"slaveWidth,omitempty"`
	SlaveHeight     *int     `json:"slaveHeight,omitempty"`
	HostSide        *string  `json:"hostSide,omitempty"`
	ReconnectMs     *int     `json:"reconnectDelayMs,omitempty"`
	CaptureKeyboard *bool    `json:"captureKeyboard,omitempty"`
	ToggleHotkey    *string  `json:"toggleHotkeyName,omitempty"`
	AutoSwitch      *bool    `json:"autoSwitch,omitempty"`
	GUIMode         *bool    `json:"guiMode,omitempty"`
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsDirName, settingsFileName), nil
}

func loadSettings() (persistedSettings, error) {
	var settings persistedSettings
	path, err := settingsPath()
	if err != nil {
		return settings, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(data, &settings)
	return settings, err
}

func (p persistedSettings) applyTo(cfg *Config) {
	if p.PortOverride != nil {
		cfg.PortOverride = *p.PortOverride
	}
	if p.MoveRateHz != nil && *p.MoveRateHz > 0 {
		cfg.MoveRateHz = *p.MoveRateHz
	}
	if p.MoveDeadzone != nil && *p.MoveDeadzone >= 0 {
		cfg.MoveDeadzone = *p.MoveDeadzone
	}
	if p.MoveSmoothing != nil && *p.MoveSmoothing >= 0 && *p.MoveSmoothing < 1 {
		cfg.MoveSmoothing = *p.MoveSmoothing
	}
	if p.AdaptiveMoves != nil {
		cfg.AdaptiveMoves = *p.AdaptiveMoves
	}
	if p.LeftwardReturn != nil {
		cfg.LeftwardReturn = *p.LeftwardReturn
	}
	if p.SlaveWidth != nil && *p.SlaveWidth > 0 {
		cfg.SlaveWidth = *p.SlaveWidth
	}
	if p.SlaveHeight != nil && *p.SlaveHeight > 0 {
		cfg.SlaveHeight = *p.SlaveHeight
	}
	if p.HostSide != nil {
		cfg.HostSide = *p.HostSide
	}
	if p.ReconnectMs != nil && *p.ReconnectMs > 0 {
		cfg.ReconnectDelay = time.Duration(*p.ReconnectMs) * time.Millisecond
	}
	if p.CaptureKeyboard != nil {
		cfg.CaptureKeyboard = *p.CaptureKeyboard
	}
	if p.ToggleHotkey != nil && *p.ToggleHotkey != "" {
		cfg.ToggleHotkey = *p.ToggleHotkey
	}
	if p.AutoSwitch != nil {
		cfg.AutoSwitch = *p.AutoSwitch
	}
	if p.GUIMode != nil {
		cfg.GUIMode = *p.GUIMode
	}
}

// Save persists the configuration atomically (temp file + rename).
func Save(cfg Config) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	reconnectMs := int(cfg.ReconnectDelay / time.Millisecond)
	settings := persistedSettings{
		Version:         2,
		PortOverride:    &cfg.PortOverride,
		MoveRateHz:      &cfg.MoveRateHz,
		MoveDeadzone:    &cfg.MoveDeadzone,
		MoveSmoothing:   &cfg.MoveSmoothing,
		AdaptiveMoves:   &cfg.AdaptiveMoves,
		LeftwardReturn:  &cfg.LeftwardReturn,
		SlaveWidth:      &cfg.SlaveWidth,
		SlaveHeight:     &cfg.SlaveHeight,
		HostSide:        &cfg.HostSide,
		ReconnectMs:     &reconnectMs,
		CaptureKeyboard: &cfg.CaptureKeyboard,
		ToggleHotkey:    &cfg.ToggleHotkey,
		AutoSwitch:      &cfg.AutoSwitch,
		GUIMode:         &cfg.GUIMode,
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

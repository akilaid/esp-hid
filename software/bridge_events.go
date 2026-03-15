//go:build windows

package main

import "time"

type bridgeEventType string

const (
	bridgeEventInfo             bridgeEventType = "info"
	bridgeEventStarting         bridgeEventType = "starting"
	bridgeEventStopping         bridgeEventType = "stopping"
	bridgeEventStopped          bridgeEventType = "stopped"
	bridgeEventCaptureError     bridgeEventType = "capture_error"
	bridgeEventSerialConnected  bridgeEventType = "serial_connected"
	bridgeEventSerialOpenFailed bridgeEventType = "serial_open_failed"
	bridgeEventSerialWriteError bridgeEventType = "serial_write_error"
)

type bridgeEvent struct {
	Type      bridgeEventType
	Message   string
	Port      string
	Timestamp time.Time
}

type bridgeEventReporter func(event bridgeEvent)

func emitBridgeEvent(reporter bridgeEventReporter, eventType bridgeEventType, port string, message string) {
	if reporter == nil {
		return
	}

	reporter(bridgeEvent{
		Type:      eventType,
		Message:   message,
		Port:      port,
		Timestamp: time.Now(),
	})
}

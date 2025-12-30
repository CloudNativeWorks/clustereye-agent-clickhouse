//go:build windows
// +build windows

package logger

import (
	"golang.org/x/sys/windows/svc/eventlog"
)

// EventLogger interface for platform-specific event logging
type EventLogger interface {
	Info(msg string) error
	Warning(msg string) error
	Error(msg string) error
	Close() error
}

type windowsEventLogger struct {
	log *eventlog.Log
}

// NewEventLogger creates a new Windows event logger
func NewEventLogger(source string) (EventLogger, error) {
	// Install event log source if not exists
	err := eventlog.InstallAsEventCreate(source, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		// Ignore if already exists
		if err.Error() != "registry key already exists" {
			return nil, err
		}
	}

	log, err := eventlog.Open(source)
	if err != nil {
		return nil, err
	}

	return &windowsEventLogger{log: log}, nil
}

func (w *windowsEventLogger) Info(msg string) error {
	return w.log.Info(1, msg)
}

func (w *windowsEventLogger) Warning(msg string) error {
	return w.log.Warning(2, msg)
}

func (w *windowsEventLogger) Error(msg string) error {
	return w.log.Error(3, msg)
}

func (w *windowsEventLogger) Close() error {
	return w.log.Close()
}

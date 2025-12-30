//go:build !windows
// +build !windows

package logger

import (
	"log/syslog"
)

// EventLogger interface for platform-specific event logging
type EventLogger interface {
	Info(msg string) error
	Warning(msg string) error
	Error(msg string) error
	Close() error
}

type syslogLogger struct {
	log *syslog.Writer
}

// NewEventLogger creates a new syslog logger for Unix-like systems
func NewEventLogger(tag string) (EventLogger, error) {
	writer, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, tag)
	if err != nil {
		return nil, err
	}

	return &syslogLogger{log: writer}, nil
}

func (s *syslogLogger) Info(msg string) error {
	return s.log.Info(msg)
}

func (s *syslogLogger) Warning(msg string) error {
	return s.log.Warning(msg)
}

func (s *syslogLogger) Error(msg string) error {
	return s.log.Err(msg)
}

func (s *syslogLogger) Close() error {
	return s.log.Close()
}

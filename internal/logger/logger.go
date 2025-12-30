package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LogLevel represents the severity level of a log message
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
	FATAL
)

// String returns the string representation of a log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARNING:
		return "WARNING"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel converts a string to LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARNING", "WARN":
		return WARNING
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO
	}
}

// Logger handles multi-level logging to various outputs
type Logger struct {
	level        LogLevel
	fileLogger   *log.Logger
	eventLogger  EventLogger
	useFileLog   bool
	useEventLog  bool
	logFile      *os.File
}

var globalLogger *Logger

// Initialize sets up the global logger
func Initialize(level string, useFileLog, useEventLog bool) error {
	logLevel := ParseLogLevel(level)

	logger := &Logger{
		level:       logLevel,
		useFileLog:  useFileLog,
		useEventLog: useEventLog,
	}

	// Setup file logging
	if useFileLog {
		logPath := getLogFilePath()
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		logger.logFile = file
		logger.fileLogger = log.New(file, "", 0)
	}

	// Setup event/syslog logging
	if useEventLog {
		eventLogger, err := NewEventLogger("ClusterEyeAgent")
		if err != nil {
			return fmt.Errorf("failed to initialize event logger: %w", err)
		}
		logger.eventLogger = eventLogger
	}

	globalLogger = logger
	return nil
}

// getLogFilePath returns the platform-specific log file path
func getLogFilePath() string {
	if runtime.GOOS == "windows" {
		// Try C:\Clustereye first, fall back to temp
		logDir := "C:\\Clustereye"
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			logDir = os.TempDir()
		}
		return filepath.Join(logDir, "clustereye-agent.log")
	}

	// Unix-like systems
	logPaths := []string{
		"/var/log/clustereye/agent.log",
		filepath.Join(os.Getenv("HOME"), ".clustereye", "agent.log"),
	}

	for _, path := range logPaths {
		dir := filepath.Dir(path)
		if _, err := os.Stat(dir); err == nil {
			return path
		}
		if err := os.MkdirAll(dir, 0755); err == nil {
			return path
		}
	}

	// Fallback to current directory
	return "clustereye-agent.log"
}

// Close closes the logger and releases resources
func Close() {
	if globalLogger != nil {
		if globalLogger.logFile != nil {
			globalLogger.logFile.Close()
		}
		if globalLogger.eventLogger != nil {
			globalLogger.eventLogger.Close()
		}
	}
}

// log writes a log message at the specified level
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] [%s] %s", timestamp, level.String(), message)

	// Always write to stdout/stderr
	if level >= ERROR {
		fmt.Fprintln(os.Stderr, logLine)
	} else {
		fmt.Println(logLine)
	}

	// Write to file if enabled
	if l.useFileLog && l.fileLogger != nil {
		l.fileLogger.Println(logLine)
	}

	// Write to event log if enabled
	if l.useEventLog && l.eventLogger != nil {
		switch level {
		case DEBUG, INFO:
			l.eventLogger.Info(message)
		case WARNING:
			l.eventLogger.Warning(message)
		case ERROR, FATAL:
			l.eventLogger.Error(message)
		}
	}

	// Exit on fatal
	if level == FATAL {
		os.Exit(1)
	}
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(DEBUG, format, args...)
	}
}

// Info logs an info message
func Info(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(INFO, format, args...)
	}
}

// Warning logs a warning message
func Warning(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(WARNING, format, args...)
	}
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(ERROR, format, args...)
	}
}

// Fatal logs a fatal message and exits
func Fatal(format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.log(FATAL, format, args...)
	} else {
		fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
		os.Exit(1)
	}
}

// GetWriter returns an io.Writer for the logger at the specified level
func GetWriter(level LogLevel) io.Writer {
	return &logWriter{level: level}
}

type logWriter struct {
	level LogLevel
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	message := string(p)
	message = strings.TrimSuffix(message, "\n")

	if globalLogger != nil {
		globalLogger.log(w.level, "%s", message)
	}

	return len(p), nil
}

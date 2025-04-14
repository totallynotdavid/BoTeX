package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

const (
	DirPerm  = 0o750 // equivalent to chmod 750
	FilePerm = 0o600 // equivalent to chmod 600
)

func ParseLogLevel(levelStr string) LogLevel {
	switch levelStr {
	case "DEBUG":
		return DEBUG
	case "WARN":
		return WARN
	case "ERROR":
		return ERROR
	default:
		return INFO
	}
}

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

type Logger struct {
	level LogLevel
	name  string
	file  *os.File
}

type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Name      string                 `json:"name,omitempty"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

type LoggerFactory struct {
	defaultLevel LogLevel
	loggers      map[string]*Logger
	mu           sync.RWMutex
	logDir       string
}

func NewLoggerFactory(defaultLevel LogLevel) *LoggerFactory {
	logDir := "logs"
	if err := os.MkdirAll(logDir, DirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logs directory: %v\n", err)
		// Fallback to current directory if we can't create logs directory
		logDir = "."
	}

	return &LoggerFactory{
		defaultLevel: defaultLevel,
		loggers:      make(map[string]*Logger),
		logDir:       logDir,
	}
}

func generateLogFilename() string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("botex_%s.log", timestamp)

	return filepath.Base(filename)
}

// GetLogger retrieves or creates a named logger from the factory.
func (f *LoggerFactory) GetLogger(name string) *Logger {
	f.mu.RLock()
	logger, exists := f.loggers[name]
	f.mu.RUnlock()

	if exists {
		return logger
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check in case another goroutine created it
	if logger, exists = f.loggers[name]; exists {
		return logger
	}

	logFile := filepath.Join(f.logDir, generateLogFilename())
	logFile = filepath.Clean(logFile)
	if !strings.HasPrefix(logFile, f.logDir) {
		logFile = filepath.Join(f.logDir, "default.log")
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, FilePerm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create log file: %v\n", err)
		// Fallback to stdout if we can't create the file
		file = os.Stdout
	}

	logger = &Logger{
		level: f.defaultLevel,
		name:  name,
		file:  file,
	}
	f.loggers[name] = logger

	return logger
}

func (l *Logger) WithLevel(level LogLevel) *Logger {
	return &Logger{
		level: level,
		name:  l.name,
	}
}

func (l *Logger) Debug(msg string, data map[string]interface{}) {
	if l.level <= DEBUG {
		l.log("DEBUG", msg, data)
	}
}

func (l *Logger) Info(msg string, data map[string]interface{}) {
	if l.level <= INFO {
		l.log("INFO", msg, data)
	}
}

func (l *Logger) Warn(msg string, data map[string]interface{}) {
	if l.level <= WARN {
		l.log("WARN", msg, data)
	}
}

func (l *Logger) Error(msg string, data map[string]interface{}) {
	if l.level <= ERROR {
		l.log("ERROR", msg, data)
	}
}

func (l *Logger) log(level, msg string, data map[string]interface{}) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Name:      l.name,
		Message:   msg,
		Data:      data,
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling log entry: %v\n", err)

		return
	}

	if _, err := fmt.Fprintf(l.file, "%s\n", string(jsonData)); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing log entry: %v\n", err)
	}
}

package logger

import (
	"encoding/json"
	"fmt"
	"os"
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
}

func NewLoggerFactory(defaultLevel LogLevel) *LoggerFactory {
	return &LoggerFactory{
		defaultLevel: defaultLevel,
		loggers:      make(map[string]*Logger),
	}
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

	logger = &Logger{
		level: f.defaultLevel,
		name:  name,
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

	if _, err := fmt.Fprintf(os.Stdout, "%s\n", string(jsonData)); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing log entry: %v\n", err)
	}
}

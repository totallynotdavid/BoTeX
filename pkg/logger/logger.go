package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

type Logger struct {
	level LogLevel
}

type LogEntry struct {
	Timestamp time.Time   `json:"timestamp"`
	Level     string      `json:"level"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
}

func NewLogger(level LogLevel) *Logger {
	return &Logger{level: level}
}

func (l *Logger) Debug(msg string, data interface{}) {
	if l.level <= DEBUG {
		l.log("DEBUG", msg, data)
	}
}

func (l *Logger) Info(msg string, data interface{}) {
	if l.level <= INFO {
		l.log("INFO", msg, data)
	}
}

func (l *Logger) Warn(msg string, data interface{}) {
	if l.level <= WARN {
		l.log("WARN", msg, data)
	}
}

func (l *Logger) Error(msg string, data interface{}) {
	if l.level <= ERROR {
		l.log("ERROR", msg, data)
	}
}

func (l *Logger) log(level string, msg string, data interface{}) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
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

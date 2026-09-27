package logger

import (
	"fmt"
	"os"
)

type Logger struct {
	name   string
	level  LogLevel
	writer *Writer
}

func NewLogger(name string, level LogLevel, writer *Writer) *Logger {
	return &Logger{
		name:   name,
		level:  level,
		writer: writer,
	}
}

func (l *Logger) WithLevel(level LogLevel) *Logger {
	return NewLogger(l.name, level, l.writer)
}

func (l *Logger) Debug(msg string, data map[string]any) {
	if l.level.IsEnabled(DEBUG) {
		l.log(DEBUG, msg, data)
	}
}

func (l *Logger) Info(msg string, data map[string]any) {
	if l.level.IsEnabled(INFO) {
		l.log(INFO, msg, data)
	}
}

func (l *Logger) Warn(msg string, data map[string]any) {
	if l.level.IsEnabled(WARN) {
		l.log(WARN, msg, data)
	}
}

func (l *Logger) Error(msg string, data map[string]any) {
	if l.level.IsEnabled(ERROR) {
		l.log(ERROR, msg, data)
	}
}

func (l *Logger) Debugf(format string, args ...any) {
	if l.level.IsEnabled(DEBUG) {
		l.log(DEBUG, fmt.Sprintf(format, args...), nil)
	}
}

func (l *Logger) Infof(format string, args ...any) {
	if l.level.IsEnabled(INFO) {
		l.log(INFO, fmt.Sprintf(format, args...), nil)
	}
}

func (l *Logger) Warnf(format string, args ...any) {
	if l.level.IsEnabled(WARN) {
		l.log(WARN, fmt.Sprintf(format, args...), nil)
	}
}

func (l *Logger) Errorf(format string, args ...any) {
	if l.level.IsEnabled(ERROR) {
		l.log(ERROR, fmt.Sprintf(format, args...), nil)
	}
}

func (l *Logger) log(level LogLevel, msg string, data map[string]any) {
	entry := NewEntry(level, l.name, msg, data)

	err := l.writer.Write(entry)
	if err != nil {
		errorMsg := fmt.Sprintf("Logger write error for %s: %v\n", l.name, err)
		fmt.Fprint(os.Stderr, errorMsg)
	}
}

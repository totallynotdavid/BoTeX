package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DirPerm  = 0o750 // allow rwxr-x--- access
	FilePerm = 0o600 // allow rw------- access
)

// Factory manages the creation and lifecycle of loggers.
// Makes sure that loggers with the same name share the same configuration
// and handles file management for log output.
type Factory struct {
	config  Config
	loggers sync.Map
	logFile *os.File
}

var ErrEmptyLogDirectory = errors.New("log directory cannot be empty")

// NewFactory initializes a logger factory with the given config.
// It ensures the log directory exists and opens a log file.
// Returns an error if the directory or file cannot be created.
func NewFactory(config Config) (*Factory, error) {
	if config.Directory == "" {
		return nil, ErrEmptyLogDirectory
	}

	err := os.MkdirAll(config.Directory, DirPerm)
	if err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	logFile, err := createLogFile(config.Directory)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	return &Factory{
		config:  config,
		logFile: logFile,
	}, nil
}

// GetLogger returns a logger with the given name.
// If one already exists, it returns that instance.
// If not, it creates a new one with the factory's config.
func (f *Factory) GetLogger(name string) *Logger {
	if logger, ok := f.loggers.Load(name); ok {
		if l, ok := logger.(*Logger); ok {
			return l
		}
	}

	writer := f.createWriter(name)
	newLogger := NewLogger(name, f.config.Level, writer)

	if actual, loaded := f.loggers.LoadOrStore(name, newLogger); loaded {
		if l, ok := actual.(*Logger); ok {
			return l
		}
	}

	return newLogger
}

func (f *Factory) CreateWhatsmeowLogger(tag, level string) *WhatsmeowLogger {
	logger := f.GetLogger(tag)

	return NewWhatsmeowLogger(logger, tag)
}

// Close releases any resources help by the factory.
// It should be called when the pkg shuts down.
func (f *Factory) Close() error {
	if f.logFile != nil && f.logFile != os.Stdout {
		err := f.logFile.Close()
		if err != nil {
			return fmt.Errorf("failed to close log file: %w", err)
		}
	}

	return nil
}

func (f *Factory) createWriter(loggerName string) *Writer {
	showInTerminal := f.shouldShowInTerminal(loggerName)
	terminalLevel := f.getTerminalLevel(loggerName)

	return NewWriter(f.logFile, showInTerminal, terminalLevel)
}

func (f *Factory) shouldShowInTerminal(loggerName string) bool {
	if !isWhatsmeowLogger(loggerName) {
		return true
	}

	return f.config.ShowWhatsmeowInTerminal
}

func (f *Factory) getTerminalLevel(loggerName string) LogLevel {
	if isWhatsmeowLogger(loggerName) && !f.config.ShowWhatsmeowInTerminal {
		return ERROR + 1
	}

	return f.config.Level
}

func createLogFile(directory string) (*os.File, error) {
	logFilename := generateLogFilename()
	logFilePath := filepath.Join(directory, logFilename)
	logFilePath = filepath.Clean(logFilePath)

	if !strings.HasPrefix(logFilePath, filepath.Clean(directory)) {
		logFilePath = filepath.Join(directory, "default.log")
	}

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, FilePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return file, nil
}

func generateLogFilename() string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	return fmt.Sprintf("botex_%s.log", timestamp)
}

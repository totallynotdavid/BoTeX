package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
)

type Writer struct {
	file          *os.File
	fileLevel     LogLevel
	terminalLevel LogLevel
	mu            sync.Mutex
}

func NewWriter(file *os.File, fileLevel, terminalLevel LogLevel) *Writer {
	return &Writer{
		file:          file,
		fileLevel:     fileLevel,
		terminalLevel: terminalLevel,
	}
}

func (w *Writer) Write(entry Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var writeErr error

	if w.file != nil && w.fileLevel.IsEnabled(entry.Level) {
		jsonData, err := entry.ToJSON()
		if err != nil {
			writeErr = fmt.Errorf("failed to marshal entry: %w", err)
		} else {
			_, err = fmt.Fprintf(w.file, "%s\n", string(jsonData))
			if err != nil {
				writeErr = fmt.Errorf("failed to write to file: %w", err)
			}
		}
	}

	if w.terminalLevel.IsEnabled(entry.Level) {
		log.Println(entry.ToTerminalFormat())
	}

	return writeErr
}

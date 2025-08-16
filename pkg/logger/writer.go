package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
)

type Writer struct {
	file           *os.File
	showInTerminal bool
	terminalLevel  LogLevel
	mu             sync.Mutex
}

func NewWriter(file *os.File, showInTerminal bool, terminalLevel LogLevel) *Writer {
	return &Writer{
		file:           file,
		showInTerminal: showInTerminal,
		terminalLevel:  terminalLevel,
	}
}

func (w *Writer) Write(entry Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var writeErr error

	if w.file != nil {
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

	if w.showInTerminal && entry.Level >= w.terminalLevel {
		log.Println(entry.ToTerminalFormat())
	}

	return writeErr
}

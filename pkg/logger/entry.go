package logger

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const defaultLogBufferSize = 128

type Entry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     LogLevel               `json:"-"`
	LevelStr  string                 `json:"level"`
	Name      string                 `json:"name,omitempty"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

func NewEntry(level LogLevel, name, message string, data map[string]interface{}) Entry {
	return Entry{
		Timestamp: time.Now(),
		Level:     level,
		LevelStr:  level.String(),
		Name:      name,
		Message:   message,
		Data:      data,
	}
}

func (e Entry) ToJSON() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal log entry: %w", err)
	}

	return data, nil
}

func (e Entry) ToTerminalFormat() string {
	var builder strings.Builder
	builder.Grow(defaultLogBufferSize)

	builder.WriteString(e.Timestamp.Format("15:04:05.000"))
	builder.WriteString(" [")

	if e.Name != "" {
		builder.WriteString(e.Name)
		builder.WriteByte(' ')
	}

	builder.WriteString(e.LevelStr)
	builder.WriteString("] ")
	builder.WriteString(e.Message)

	if len(e.Data) > 0 {
		dataBytes, err := json.Marshal(e.Data)
		if err == nil {
			builder.WriteByte(' ')
			builder.Write(dataBytes)
		}
	}

	return builder.String()
}

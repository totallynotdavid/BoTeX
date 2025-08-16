package logger

import "strings"

type LogLevel int8

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

func ParseLogLevel(levelStr string) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(levelStr)) {
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

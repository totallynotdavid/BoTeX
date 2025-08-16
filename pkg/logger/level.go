package logger

import "strings"

type LogLevel int8

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	DISABLED
)

func ParseLogLevel(levelStr string) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(levelStr)) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN":
		return WARN
	case "ERROR":
		return ERROR
	case "DISABLED":
		return DISABLED
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
	case DISABLED:
		return "DISABLED"
	default:
		return "UNKNOWN"
	}
}

func (l LogLevel) IsEnabled(level LogLevel) bool {
	if l == DISABLED {
		return false
	}

	return level >= l
}

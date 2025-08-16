package logger

import (
	"os"
	"strings"
)

type Config struct {
	Level                   LogLevel
	ShowWhatsmeowInTerminal bool
	Directory               string
}

func LoadFromEnv() Config {
	return Config{
		Level:                   ParseLogLevel(getEnv("BOTEX_LOG_LEVEL", "INFO")),
		ShowWhatsmeowInTerminal: parseBool(getEnv("BOTEX_SHOW_WHATSMEOW_LOGS", "false")),
		Directory:               getEnv("BOTEX_LOG_DIR", "logs"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}

func parseBool(value string) bool {
	return strings.EqualFold(value, "true") || value == "1"
}

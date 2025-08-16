package logger

import (
	"botex/pkg/util"
)

type Config struct {
	Level                   LogLevel
	ShowWhatsmeowInTerminal bool
	Directory               string
}

func LoadFromEnv() Config {
	return Config{
		Level:                   ParseLogLevel(util.GetEnv("BOTEX_LOG_LEVEL", "INFO")),
		ShowWhatsmeowInTerminal: util.ParseBool(util.GetEnv("BOTEX_SHOW_WHATSMEOW_LOGS", "false")),
		Directory:               util.GetEnv("BOTEX_LOG_DIR", "logs"),
	}
}

package config

import (
	"os"
	"time"
)

type Config struct {
	DBPath        string
	LogLevel      string
	TempDir       string
	MaxImageSize  int64
	CleanupPeriod time.Duration
}

func Load() *Config {
	return &Config{
		DBPath:        getEnv("BOTEX_DB_PATH", "file:examplestore.db?_foreign_keys=on"),
		LogLevel:      getEnv("BOTEX_LOG_LEVEL", "WARN"),
		TempDir:       getEnv("BOTEX_TEMP_DIR", os.TempDir()),
		MaxImageSize:  5 * 1024 * 1024, // 5MB
		CleanupPeriod: 1 * time.Hour,
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

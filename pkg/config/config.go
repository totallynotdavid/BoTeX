package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	DBPath        string
	LogLevel      string
	TempDir       string
	MaxImageSize  int64
	CleanupPeriod time.Duration
	MaxConcurrent int
	RateLimit     struct {
		Requests int
		Period   time.Duration
	}
}

func Load() *Config {
	cfg := &Config{
		DBPath:        getEnv("BOTEX_DB_PATH", "file:examplestore.db?_foreign_keys=on&_journal_mode=WAL"),
		LogLevel:      getEnv("BOTEX_LOG_LEVEL", "WARN"),
		TempDir:       getEnv("BOTEX_TEMP_DIR", os.TempDir()),
		MaxImageSize:  5 * 1024 * 1024, // 5MB
		CleanupPeriod: 1 * time.Hour,
		MaxConcurrent: 10,
	}

	// Configure rate limiting
	cfg.RateLimit.Requests = 5
	cfg.RateLimit.Period = 1 * time.Minute

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func (c *Config) Validate() error {
	if c.MaxImageSize <= 0 {
		return fmt.Errorf("MaxImageSize must be positive")
	}
	if c.CleanupPeriod <= 0 {
		return fmt.Errorf("CleanupPeriod must be positive")
	}
	if c.MaxConcurrent <= 0 {
		return fmt.Errorf("MaxConcurrent must be positive")
	}
	if c.RateLimit.Requests <= 0 {
		return fmt.Errorf("RateLimit.Requests must be positive")
	}
	if c.RateLimit.Period <= 0 {
		return fmt.Errorf("RateLimit.Period must be positive")
	}
	return nil
}

package config

import (
	"errors"
	"os"
	"time"
)

type Config struct {
	DBPath        string
	LogLevel      string
	TempDir       string
	MaxImageSize  int64
	MaxConcurrent int
	RateLimit     struct {
		Requests             int
		Period               time.Duration
		NotificationCooldown time.Duration
		CleanupInterval      time.Duration
	}
}

func Load() *Config {
	cfg := &Config{
		DBPath:        getEnv("BOTEX_DB_PATH", "file:botex.db?_foreign_keys=on&_journal_mode=WAL"),
		LogLevel:      getEnv("BOTEX_LOG_LEVEL", "INFO"),
		TempDir:       getEnv("BOTEX_TEMP_DIR", os.TempDir()),
		MaxImageSize:  5 * 1024 * 1024, // 5MB
		MaxConcurrent: 10,
	}

	// Rate limiting configuration
	cfg.RateLimit.Requests = 5
	cfg.RateLimit.Period = 1 * time.Minute
	cfg.RateLimit.NotificationCooldown = 5 * time.Minute
	cfg.RateLimit.CleanupInterval = 1 * time.Hour

	return cfg
}

func (c *Config) Validate() error {
	if c.MaxImageSize <= 0 {
		return errors.New("MaxImageSize must be positive")
	}
	if c.MaxConcurrent <= 0 {
		return errors.New("MaxConcurrent must be positive")
	}
	if c.RateLimit.Requests <= 0 {
		return errors.New("RateLimit.Requests must be positive")
	}
	if c.RateLimit.Period <= 0 {
		return errors.New("RateLimit.Period must be positive")
	}
	if c.RateLimit.NotificationCooldown <= 0 {
		return errors.New("RateLimit.NotificationCooldown must be positive")
	}
	if c.RateLimit.CleanupInterval <= 0 {
		return errors.New("RateLimit.CleanupInterval must be positive")
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}

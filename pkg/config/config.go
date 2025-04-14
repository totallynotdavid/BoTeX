package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

const (
	// Size constants.
	KB = 1024
	MB = KB * 1024

	// Default configuration values.
	DefaultMaxImageSize  = 5 * MB
	DefaultMaxConcurrent = 10

	// Rate limiting defaults.
	DefaultRateLimitRequests             = 5
	DefaultRateLimitPeriod               = 1 * time.Minute
	DefaultRateLimitNotificationCooldown = 5 * time.Minute
	DefaultRateLimitCleanupInterval      = 1 * time.Hour

	// Default timing configuration.
	DefaultTimingLevel        = "disabled"
	DefaultTimingLogThreshold = 100 * time.Millisecond
)

var (
	ErrMaxImageSizeMustBePositive           = errors.New("MaxImageSize must be positive")
	ErrMaxConcurrentMustBePositive          = errors.New("MaxConcurrent must be positive")
	ErrRateLimitRequestsMustBePositive      = errors.New("RateLimit.Requests must be positive")
	ErrRateLimitPeriodMustBePositive        = errors.New("RateLimit.Period must be positive")
	ErrRateLimitNotificationCooldownInvalid = errors.New("RateLimit.NotificationCooldown must be positive")
	ErrRateLimitCleanupIntervalInvalid      = errors.New("RateLimit.CleanupInterval must be positive")
	ErrTimingLogThresholdInvalid            = errors.New("Timing.LogThreshold must be positive")
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
	Timing struct {
		Level        string
		LogThreshold time.Duration
	}
	PDFLatexPath string
	ConvertPath  string
	CWebPPath    string
}

func Load() *Config {
	cfg := &Config{
		DBPath:        getEnv("BOTEX_DB_PATH", "file:botex.db?_foreign_keys=on&_journal_mode=WAL"),
		LogLevel:      getEnv("BOTEX_LOG_LEVEL", "INFO"),
		TempDir:       getEnv("BOTEX_TEMP_DIR", os.TempDir()),
		MaxImageSize:  DefaultMaxImageSize,
		MaxConcurrent: DefaultMaxConcurrent,
		PDFLatexPath:  getEnv("BOTEX_PDFLATEX_PATH", ""),
		ConvertPath:   getEnv("BOTEX_CONVERT_PATH", ""),
		CWebPPath:     getEnv("BOTEX_CWEBP_PATH", ""),
	}

	cfg.RateLimit.Requests = DefaultRateLimitRequests
	cfg.RateLimit.Period = DefaultRateLimitPeriod
	cfg.RateLimit.NotificationCooldown = DefaultRateLimitNotificationCooldown
	cfg.RateLimit.CleanupInterval = DefaultRateLimitCleanupInterval

	cfg.Timing.Level = getEnv("BOTEX_TIMING_LEVEL", DefaultTimingLevel)
	thresholdStr := getEnv("BOTEX_TIMING_THRESHOLD", "100")
	threshold, err := strconv.Atoi(thresholdStr)
	if err == nil && threshold > 0 {
		cfg.Timing.LogThreshold = time.Duration(threshold) * time.Millisecond
	} else {
		cfg.Timing.LogThreshold = DefaultTimingLogThreshold
	}

	return cfg
}

func (c *Config) Validate() error {
	if c.MaxImageSize <= 0 {
		return ErrMaxImageSizeMustBePositive
	}
	if c.MaxConcurrent <= 0 {
		return ErrMaxConcurrentMustBePositive
	}
	if c.RateLimit.Requests <= 0 {
		return ErrRateLimitRequestsMustBePositive
	}
	if c.RateLimit.Period <= 0 {
		return ErrRateLimitPeriodMustBePositive
	}
	if c.RateLimit.NotificationCooldown <= 0 {
		return ErrRateLimitNotificationCooldownInvalid
	}
	if c.RateLimit.CleanupInterval <= 0 {
		return ErrRateLimitCleanupIntervalInvalid
	}
	if c.Timing.LogThreshold <= 0 {
		return ErrTimingLogThresholdInvalid
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}

package config

import (
	"errors"
	"os"
	"strconv"
	"time"

	"botex/pkg/logger"
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
	ErrTimingLogThresholdInvalid            = errors.New("Timing.LogThreshold must be non-negative")
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

func Load(logger *logger.Logger) *Config {
	cfg := &Config{
		DBPath:        getEnv(logger, "BOTEX_DB_PATH", "file:botex.db?_foreign_keys=on&_journal_mode=WAL"),
		LogLevel:      getEnv(logger, "BOTEX_LOG_LEVEL", "INFO"),
		TempDir:       getEnv(logger, "BOTEX_TEMP_DIR", os.TempDir()),
		MaxImageSize:  DefaultMaxImageSize,
		MaxConcurrent: DefaultMaxConcurrent,
		PDFLatexPath:  getEnv(logger, "BOTEX_PDFLATEX_PATH", ""),
		ConvertPath:   getEnv(logger, "BOTEX_CONVERT_PATH", ""),
		CWebPPath:     getEnv(logger, "BOTEX_CWEBP_PATH", ""),
	}

	cfg.RateLimit.Requests = DefaultRateLimitRequests
	cfg.RateLimit.Period = DefaultRateLimitPeriod
	cfg.RateLimit.NotificationCooldown = DefaultRateLimitNotificationCooldown
	cfg.RateLimit.CleanupInterval = DefaultRateLimitCleanupInterval

	cfg.Timing.Level = getEnv(logger, "BOTEX_TIMING_LEVEL", DefaultTimingLevel)
	thresholdStr := getEnv(logger, "BOTEX_TIMING_THRESHOLD", "100")
	threshold, err := strconv.Atoi(thresholdStr)
	if err == nil {
		cfg.Timing.LogThreshold = time.Duration(threshold) * time.Millisecond
	} else {
		cfg.Timing.LogThreshold = DefaultTimingLogThreshold
	}

	// Log the timing configuration
	logger.Info("Timing configuration loaded", map[string]interface{}{
		"level":     cfg.Timing.Level,
		"threshold": cfg.Timing.LogThreshold,
	})

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
	if c.Timing.LogThreshold < 0 {
		return ErrTimingLogThresholdInvalid
	}

	return nil
}

func getEnv(logger *logger.Logger, key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	logger.Debug("Loading env var", map[string]interface{}{
		"key":     key,
		"exists":  exists,
		"value":   value,
		"default": defaultValue,
	})

	if exists {
		return value
	}

	return defaultValue
}

package config

import (
	"errors"
	"os"
	"strconv"
	"time"

	"botex/pkg/logger"
	"botex/pkg/util"
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
	TempDir       string
	MaxImageSize  int64
	MaxConcurrent int

	RateLimit struct {
		Requests             int
		Period               time.Duration
		NotificationCooldown time.Duration
		CleanupInterval      time.Duration
	}

	Timing struct {
		Level        string
		LogThreshold time.Duration
	}

	Logging logger.Config

	PDFLatexPath string
	ConvertPath  string
	CWebPPath    string

	// Auth database configuration
	Auth struct {
		DatabasePath        string
		DefaultUserRank     string
		EnableWhatsAppAdmin bool
		ValidateSchema      bool
	}
}

func Load() *Config {
	cfg := &Config{
		DBPath:        util.GetEnv("BOTEX_DB_PATH", "file:botex.db?_foreign_keys=on&_journal_mode=WAL"),
		TempDir:       util.GetEnv("BOTEX_TEMP_DIR", os.TempDir()),
		MaxImageSize:  DefaultMaxImageSize,
		MaxConcurrent: DefaultMaxConcurrent,
		PDFLatexPath:  util.GetEnv("BOTEX_PDFLATEX_PATH", ""),
		ConvertPath:   util.GetEnv("BOTEX_CONVERT_PATH", ""),
		CWebPPath:     util.GetEnv("BOTEX_CWEBP_PATH", ""),
		Logging:       logger.LoadFromEnv(),
	}

	cfg.RateLimit.Requests = DefaultRateLimitRequests
	cfg.RateLimit.Period = DefaultRateLimitPeriod
	cfg.RateLimit.NotificationCooldown = DefaultRateLimitNotificationCooldown
	cfg.RateLimit.CleanupInterval = DefaultRateLimitCleanupInterval

	cfg.Timing.Level = util.GetEnv("BOTEX_TIMING_LEVEL", DefaultTimingLevel)

	thresholdStr := util.GetEnv("BOTEX_TIMING_THRESHOLD", "100")

	threshold, err := strconv.Atoi(thresholdStr)
	if err == nil {
		cfg.Timing.LogThreshold = time.Duration(threshold) * time.Millisecond
	} else {
		cfg.Timing.LogThreshold = DefaultTimingLogThreshold
	}

	// Load auth configuration
	cfg.Auth.DatabasePath = util.GetEnv("BOTEX_AUTH_DB_PATH", cfg.DBPath)
	cfg.Auth.DefaultUserRank = util.GetEnv("BOTEX_AUTH_DEFAULT_RANK", "basic")
	cfg.Auth.EnableWhatsAppAdmin = util.GetEnv("BOTEX_AUTH_ENABLE_WHATSAPP_ADMIN", "true") == "true"
	cfg.Auth.ValidateSchema = util.GetEnv("BOTEX_AUTH_VALIDATE_SCHEMA", "true") == "true"

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

	// Validate auth configuration
	if c.Auth.DatabasePath == "" {
		c.Auth.DatabasePath = c.DBPath // Fallback to main DB path
	}

	if c.Auth.DefaultUserRank == "" {
		c.Auth.DefaultUserRank = "basic"
	}

	return nil
}

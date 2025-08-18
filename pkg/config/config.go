package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"botex/pkg/logger"
	"botex/pkg/util"
	"github.com/joho/godotenv"
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

	Auth struct {
		DatabasePath        string
		DefaultUserRank     string
		EnableWhatsAppAdmin bool
		ValidateSchema      bool
	}
}

type envLoader struct {
	cfg *Config
}

func (e *envLoader) loadBasic() {
	e.cfg.DBPath = util.GetEnv("BOTEX_DB_PATH", "file:botex.db?_foreign_keys=on&_journal_mode=WAL")
	e.cfg.TempDir = util.GetEnv("BOTEX_TEMP_DIR", os.TempDir())
	e.cfg.MaxImageSize = util.GetEnvInt64("BOTEX_MAX_IMAGE_SIZE", DefaultMaxImageSize)
	e.cfg.MaxConcurrent = util.GetEnvInt("BOTEX_MAX_CONCURRENT", DefaultMaxConcurrent)
	e.cfg.PDFLatexPath = util.GetEnv("BOTEX_PDFLATEX_PATH", "")
	e.cfg.ConvertPath = util.GetEnv("BOTEX_CONVERT_PATH", "")
	e.cfg.CWebPPath = util.GetEnv("BOTEX_CWEBP_PATH", "")
}

func (e *envLoader) loadRateLimit() {
	e.cfg.RateLimit.Requests = util.GetEnvInt("BOTEX_RATE_LIMIT_REQUESTS", DefaultRateLimitRequests)
	e.cfg.RateLimit.Period = util.GetEnvDuration("BOTEX_RATE_LIMIT_PERIOD", DefaultRateLimitPeriod)
	e.cfg.RateLimit.NotificationCooldown = util.GetEnvDuration("BOTEX_RATE_LIMIT_NOTIFICATION_COOLDOWN", DefaultRateLimitNotificationCooldown)
	e.cfg.RateLimit.CleanupInterval = util.GetEnvDuration("BOTEX_RATE_LIMIT_CLEANUP_INTERVAL", DefaultRateLimitCleanupInterval)
}

func (e *envLoader) loadTiming() {
	e.cfg.Timing.Level = util.GetEnv("BOTEX_TIMING_LEVEL", DefaultTimingLevel)
	e.cfg.Timing.LogThreshold = util.GetEnvDuration("BOTEX_TIMING_THRESHOLD", DefaultTimingLogThreshold)
}

func (e *envLoader) loadAuth() {
	e.cfg.Auth.DatabasePath = util.GetEnv("BOTEX_AUTH_DB_PATH", e.cfg.DBPath)
	e.cfg.Auth.DefaultUserRank = util.GetEnv("BOTEX_AUTH_DEFAULT_RANK", "basic")
	e.cfg.Auth.EnableWhatsAppAdmin = util.GetEnvBool("BOTEX_AUTH_ENABLE_WHATSAPP_ADMIN", true)
	e.cfg.Auth.ValidateSchema = util.GetEnvBool("BOTEX_AUTH_VALIDATE_SCHEMA", true)
}

func (e *envLoader) loadAll() {
	e.loadBasic()
	e.loadRateLimit()
	e.loadTiming()
	e.loadAuth()
}

func Load() (*Config, error) {
	err := godotenv.Load()
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load .env file: %w", err)
	}

	cfg := &Config{
		Logging: logger.LoadFromEnv(),
	}

	loader := &envLoader{cfg: cfg}
	loader.loadAll()

	err = cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
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

	if c.Auth.DatabasePath == "" {
		c.Auth.DatabasePath = c.DBPath
	}

	if c.Auth.DefaultUserRank == "" {
		c.Auth.DefaultUserRank = "basic"
	}

	return nil
}

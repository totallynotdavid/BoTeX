package timing

import (
	"strings"

	"botex/pkg/config"
	"botex/pkg/logger"
)

func NewTrackerFromConfig(cfg *config.Config, logger *logger.Logger) *Tracker {
	return NewTracker(
		Config{
			Level:        ParseLevel(cfg.Timing.Level),
			LogThreshold: cfg.Timing.LogThreshold,
		},
		logger,
	)
}

func ParseLevel(levelStr string) Level {
	switch strings.ToLower(levelStr) {
	case "basic":
		return Basic
	case "detailed":
		return Detailed
	case "debug":
		return Debug
	default:
		return Disabled
	}
}

package timing

import (
	"strings"

	"botex/pkg/config"
	"botex/pkg/logger"
)

func NewTrackerFromConfig(cfg *config.Config, logger *logger.Logger) *Tracker {
	logger.Debug("Creating timing tracker", map[string]interface{}{
		"level":     cfg.Timing.Level,
		"threshold": cfg.Timing.LogThreshold,
	})

	return NewTracker(
		Config{
			Level:        ParseLevel(cfg.Timing.Level, logger),
			LogThreshold: cfg.Timing.LogThreshold,
		},
		logger,
	)
}

func ParseLevel(levelStr string, logger *logger.Logger) Level {
	logger.Debug("Parsing timing level", map[string]interface{}{
		"input": levelStr,
	})

	level := Disabled
	switch strings.ToLower(levelStr) {
	case "disabled":
		level = Disabled
	case "basic":
		level = Basic
	case "detailed":
		level = Detailed
	case "debug":
		level = Debug
	}

	logger.Debug("Parsed timing level", map[string]interface{}{
		"level": level,
	})

	return level
}

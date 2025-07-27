package timing

import (
	"strings"

	"botex/pkg/config"
	"botex/pkg/logger"
)

func NewTrackerFromConfig(cfg *config.Config, log *logger.Logger) *Tracker {
	log.Debug("Creating timing tracker", map[string]interface{}{
		"level":     cfg.Timing.Level,
		"threshold": cfg.Timing.LogThreshold,
	})

	return NewTracker(
		Config{
			Level:        ParseLevel(cfg.Timing.Level, log),
			LogThreshold: cfg.Timing.LogThreshold,
		},
		log,
	)
}

func ParseLevel(levelStr string, log *logger.Logger) Level {
	log.Debug("Parsing timing level", map[string]interface{}{
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

	log.Debug("Parsed timing level", map[string]interface{}{
		"level": level,
	})

	return level
}

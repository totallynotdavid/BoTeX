package timing

import (
	"context"
	"time"

	"botex/pkg/logger"
)

type Level int

const (
	// Disabled turns off performance tracking.
	Disabled Level = iota
	// Basic tracks only high-level operations.
	Basic
	// Detailed tracks all significant operations.
	Detailed
	// Debug tracks everything including internal operations.
	Debug
)

type Config struct {
	Level        Level
	LogThreshold time.Duration
}

type Tracker struct {
	config Config
	logger *logger.Logger
}

// NewTracker creates a new performance tracker with the given configuration.
func NewTracker(config Config, log *logger.Logger) *Tracker {
	return &Tracker{
		config: config,
		logger: log,
	}
}

// Track executes the given function and records its execution time if tracking is enabled.
func (t *Tracker) Track(ctx context.Context, operation string, level Level, operationFunc func(context.Context) error) error {
	if t.config.Level == Disabled || (t.config.Level < level && t.config.Level != Debug) {
		// Skip tracking if tracking is disabled or if the requested level is higher than configured
		// (unless we're in debug mode, in which case we track everything)
		return operationFunc(ctx)
	}

	start := time.Now()
	err := operationFunc(ctx)
	duration := time.Since(start)

	// Only log if duration exceeds threshold
	if duration >= t.config.LogThreshold {
		t.logger.Info("Performance tracking", map[string]interface{}{
			"operation": operation,
			"duration":  duration.String(),
			"level":     level,
		})
	}

	return err
}

// TrackCommand is a specialized version of Track for command execution.
func (t *Tracker) TrackCommand(ctx context.Context, commandName string, fn func(context.Context) error) error {
	return t.Track(ctx, "command_execution:"+commandName, Basic, fn)
}

// TrackSubOperation is for tracking operations within a command.
func (t *Tracker) TrackSubOperation(ctx context.Context, operation string, fn func(context.Context) error) error {
	return t.Track(ctx, operation, Detailed, fn)
}

// TrackInternal is for tracking internal operations (debug level).
func (t *Tracker) TrackInternal(ctx context.Context, operation string, fn func(context.Context) error) error {
	return t.Track(ctx, operation, Debug, fn)
}

// WithOperation returns a derived context with the current operation name.
func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey, operation)
}

// GetOperation retrieves the current operation name from context.
func GetOperation(ctx context.Context) string {
	if val, ok := ctx.Value(operationKey).(string); ok {
		return val
	}

	return "unknown"
}

type contextKey string

const operationKey contextKey = "current_operation"

package ratelimit

import (
	"time"
)

const (
	// cleanupPeriodDivisor determines how frequently the auto-cleaner runs
	// relative to the rate limit period. A value of 2 means it runs twice
	// as often as the rate limit period.
	cleanupPeriodDivisor = 2
)

type Manager struct {
	Limiter   *Limiter
	Notifier  *Notifier
	AutoClean *AutoCleaner
}

func NewManager(requests int, period time.Duration) *Manager {
	limiter := NewLimiter(requests, period)
	notifier := NewNotifier(period)
	cleaner := NewAutoCleaner(period / cleanupPeriodDivisor)

	// Register components for automatic cleanup
	cleaner.Register(limiter)
	cleaner.Register(notifier)

	return &Manager{
		Limiter:   limiter,
		Notifier:  notifier,
		AutoClean: cleaner,
	}
}

// Stop cleans up resources used by the rate limit manager.
func (m *Manager) Stop() {
	m.AutoClean.Stop()
}

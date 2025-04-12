package ratelimit

import (
	"time"
)

type Manager struct {
	Limiter   *Limiter
	Notifier  *Notifier
	AutoClean *AutoCleaner
}

func NewManager(requests int, period time.Duration) *Manager {
	limiter := NewLimiter(requests, period)
	notifier := NewNotifier(period)
	cleaner := NewAutoCleaner(period / 2)

	// Register components for automatic cleanup
	cleaner.Register(limiter)
	cleaner.Register(notifier)

	return &Manager{
		Limiter:   limiter,
		Notifier:  notifier,
		AutoClean: cleaner,
	}
}

// Stop cleans up resources used by the rate limit manager
func (m *Manager) Stop() {
	m.AutoClean.Stop()
}

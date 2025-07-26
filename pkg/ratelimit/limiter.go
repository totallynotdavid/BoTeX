package ratelimit

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type Result struct {
	Allowed    bool
	ResetAfter time.Duration
}

type Limiter struct {
	mu          sync.Mutex
	requests    map[types.JID][]time.Time
	maxRequests int
	period      time.Duration
}

func NewLimiter(maxRequests int, period time.Duration) *Limiter {
	return &Limiter{
		mu:          sync.Mutex{},
		requests:    make(map[types.JID][]time.Time),
		maxRequests: maxRequests,
		period:      period,
	}
}

func (l *Limiter) Check(user types.JID) Result {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	requests := l.requests[user]

	// Filter valid requests and find earliest timestamp
	var (
		validRequests []time.Time
		earliest      time.Time
	)

	for _, t := range requests {
		if now.Sub(t) <= l.period {
			validRequests = append(validRequests, t)
			if earliest.IsZero() || t.Before(earliest) {
				earliest = t
			}
		}
	}

	// Calculate reset time
	resetAfter := l.period
	if len(validRequests) > 0 {
		resetAfter = l.period - now.Sub(earliest)
		if resetAfter < 0 {
			resetAfter = 0
		}
	}

	allowed := len(validRequests) < l.maxRequests
	if allowed {
		validRequests = append(validRequests, now)
		l.requests[user] = validRequests
	}

	return Result{
		Allowed:    allowed,
		ResetAfter: resetAfter,
	}
}

func (l *Limiter) Allow(user types.JID) bool {
	return l.Check(user).Allowed
}

func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for user, requests := range l.requests {
		var valid []time.Time

		for _, t := range requests {
			if now.Sub(t) <= l.period {
				valid = append(valid, t)
			}
		}

		if len(valid) == 0 {
			delete(l.requests, user)
		} else {
			l.requests[user] = valid
		}
	}
}

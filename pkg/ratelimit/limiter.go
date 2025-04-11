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

func NewLimiter(requests int, period time.Duration) *Limiter {
	return &Limiter{
		requests:    make(map[types.JID][]time.Time),
		maxRequests: requests,
		period:      period,
	}
}

// Check performs a rate limit check for a user
func (l *Limiter) Check(user types.JID) Result {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	requests := l.requests[user]

	// Count valid requests and find earliest request time
	var validRequests []time.Time
	var earliestTime time.Time

	for _, t := range requests {
		age := now.Sub(t)
		if age <= l.period {
			validRequests = append(validRequests, t)
			if earliestTime.IsZero() || t.Before(earliestTime) {
				earliestTime = t
			}
		}
	}

	// Calculate time until reset
	var resetAfter time.Duration
	if !earliestTime.IsZero() {
		resetAfter = l.period - now.Sub(earliestTime)
		if resetAfter < 0 {
			resetAfter = 0
		}
	}

	// Check if rate limited
	allowed := len(validRequests) < l.maxRequests

	// If allowed, record this request
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

// Cleanup removes expired request records
func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for user, requests := range l.requests {
		var validRequests []time.Time
		for _, t := range requests {
			if now.Sub(t) <= l.period {
				validRequests = append(validRequests, t)
			}
		}
		if len(validRequests) == 0 {
			delete(l.requests, user)
		} else {
			l.requests[user] = validRequests
		}
	}
}

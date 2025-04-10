package ratelimit

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type Limiter struct {
	mu       sync.Mutex
	requests map[types.JID][]time.Time
	config   struct {
		requests int
		period   time.Duration
	}
}

func NewLimiter(requests int, period time.Duration) *Limiter {
	return &Limiter{
		requests: make(map[types.JID][]time.Time),
		config: struct {
			requests int
			period   time.Duration
		}{
			requests: requests,
			period:   period,
		},
	}
}

func (l *Limiter) Allow(user types.JID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	requests := l.requests[user]

	// Remove expired requests
	var validRequests []time.Time
	for _, t := range requests {
		if now.Sub(t) <= l.config.period {
			validRequests = append(validRequests, t)
		}
	}

	if len(validRequests) >= l.config.requests {
		return false
	}

	validRequests = append(validRequests, now)
	l.requests[user] = validRequests
	return true
}

func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for user, requests := range l.requests {
		var validRequests []time.Time
		for _, t := range requests {
			if now.Sub(t) <= l.config.period {
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

// Package ratelimit limits how often each user may send a command.
package ratelimit

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrInvalidLimit = errors.New("invalid rate limit")

// Result is the outcome of one Check.
type Result struct {
	Allowed bool
	// ResetAfter is how long until the user's oldest counted request leaves
	// the window.
	ResetAfter time.Duration
	// Notify is set on a denial when the user has not been told about the
	// limit within the cooldown, so the caller sends one notice per window
	// instead of one per message.
	Notify bool
}

// Limiter allows each user a number of requests per sliding window. It is
// safe for concurrent use.
type Limiter struct {
	requests int
	period   time.Duration
	cooldown time.Duration

	mu        sync.Mutex
	hits      map[string][]time.Time
	notified  map[string]time.Time
	lastSweep time.Time
}

// NewLimiter allows requests per period for each user and asks for a notice
// at most once per cooldown while the user stays over the limit. It fails with
// ErrInvalidLimit unless requests and period are positive, since such a
// limiter would deny everyone or index an empty window. To not limit, do not
// build a limiter.
func NewLimiter(requests int, period, cooldown time.Duration) (*Limiter, error) {
	if requests <= 0 || period <= 0 {
		return nil, fmt.Errorf("%w: %d requests per %v", ErrInvalidLimit, requests, period)
	}

	return &Limiter{
		requests: requests,
		period:   period,
		cooldown: cooldown,
		hits:     make(map[string][]time.Time),
		notified: make(map[string]time.Time),
	}, nil
}

// Check counts a request from user when the limit allows it. A denied request
// is not counted, so a user who keeps sending is admitted again as soon as
// the oldest counted request expires.
func (l *Limiter) Check(user string) Result {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.sweep(now)

	window := l.window(user, now)
	if len(window) < l.requests {
		l.hits[user] = append(window, now)
		delete(l.notified, user)

		return Result{Allowed: true, ResetAfter: l.period}
	}

	result := Result{ResetAfter: l.period - now.Sub(window[0])}

	last, told := l.notified[user]
	if !told || now.Sub(last) >= l.cooldown {
		l.notified[user] = now
		result.Notify = true
	}

	return result
}

// window drops user's requests older than the period and returns the rest.
func (l *Limiter) window(user string, now time.Time) []time.Time {
	hits := l.hits[user]

	keep := 0
	for keep < len(hits) && now.Sub(hits[keep]) >= l.period {
		keep++
	}

	hits = hits[keep:]
	if len(hits) == 0 {
		delete(l.hits, user)
	} else {
		l.hits[user] = hits
	}

	return hits
}

// sweep forgets users who have been idle for a period, at most once per
// period, so the maps stay bounded without a cleanup goroutine.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.period {
		return
	}

	l.lastSweep = now

	for user := range l.hits {
		l.window(user, now)
	}

	for user, last := range l.notified {
		if now.Sub(last) >= l.cooldown {
			delete(l.notified, user)
		}
	}
}

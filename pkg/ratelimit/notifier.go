package ratelimit

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type Notifier struct {
	mu           sync.RWMutex
	notifiedTime map[types.JID]time.Time
	notifyPeriod time.Duration
}

func NewNotifier(notifyPeriod time.Duration) *Notifier {
	return &Notifier{
		notifiedTime: make(map[types.JID]time.Time),
		notifyPeriod: notifyPeriod,
	}
}

// ShouldNotify checks if a user should be notified about rate limiting
// Returns true if this is the first notification within the notify period
func (n *Notifier) ShouldNotify(user types.JID) bool {
	n.mu.RLock()
	notifyTime, exists := n.notifiedTime[user]
	n.mu.RUnlock()

	// If no record exists or the notification period has elapsed, we should notify
	shouldNotify := !exists || time.Since(notifyTime) > n.notifyPeriod

	// If we should notify, record this notification
	if shouldNotify {
		n.mu.Lock()
		n.notifiedTime[user] = time.Now()
		n.mu.Unlock()
	}

	return shouldNotify
}

// ClearNotification removes the notification status for a user
func (n *Notifier) ClearNotification(user types.JID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.notifiedTime, user)
}

// Cleanup removes expired notification records
func (n *Notifier) Cleanup() {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()
	for user, notifyTime := range n.notifiedTime {
		if now.Sub(notifyTime) > n.notifyPeriod {
			delete(n.notifiedTime, user)
		}
	}
}

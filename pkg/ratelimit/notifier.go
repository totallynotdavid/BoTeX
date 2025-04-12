package ratelimit

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type Notifier struct {
	mu           sync.RWMutex
	notifiedTime map[types.JID]time.Time
	cooldown     time.Duration
}

func NewNotifier(cooldown time.Duration) *Notifier {
	return &Notifier{
		mu:           sync.RWMutex{},
		notifiedTime: make(map[types.JID]time.Time),
		cooldown:     cooldown,
	}
}

func (n *Notifier) ShouldNotify(user types.JID) bool {
	n.mu.RLock()
	lastNotify, exists := n.notifiedTime[user]
	n.mu.RUnlock()

	if !exists || time.Since(lastNotify) > n.cooldown {
		n.mu.Lock()
		n.notifiedTime[user] = time.Now()
		n.mu.Unlock()

		return true
	}

	return false
}

func (n *Notifier) Clear(user types.JID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.notifiedTime, user)
}

func (n *Notifier) Cleanup() {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()
	for user, lastNotify := range n.notifiedTime {
		if now.Sub(lastNotify) > n.cooldown {
			delete(n.notifiedTime, user)
		}
	}
}

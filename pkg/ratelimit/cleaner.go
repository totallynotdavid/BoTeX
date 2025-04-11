package ratelimit

import (
	"sync"
	"time"
)

type Cleanable interface {
	Cleanup()
}

type AutoCleaner struct {
	mu          sync.Mutex
	cleanables  []Cleanable
	ticker      *time.Ticker
	stopCleaner chan struct{}
	isRunning   bool
}

func NewAutoCleaner(interval time.Duration) *AutoCleaner {
	cleaner := &AutoCleaner{
		cleanables:  make([]Cleanable, 0),
		ticker:      time.NewTicker(interval),
		stopCleaner: make(chan struct{}),
	}
	return cleaner
}

// Register adds a cleanable object to be automatically cleaned
func (c *AutoCleaner) Register(cleanable Cleanable) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanables = append(c.cleanables, cleanable)

	// Start the cleaner if it's not already running
	if !c.isRunning {
		go c.runCleanup()
		c.isRunning = true
	}
}

// Stop halts the periodic cleanup
func (c *AutoCleaner) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunning {
		c.ticker.Stop()
		close(c.stopCleaner)
		c.isRunning = false
	}
}

// CleanAll triggers cleanup for all registered objects
func (c *AutoCleaner) CleanAll() {
	c.mu.Lock()
	cleanables := make([]Cleanable, len(c.cleanables))
	copy(cleanables, c.cleanables)
	c.mu.Unlock()

	for _, cleanable := range cleanables {
		cleanable.Cleanup()
	}
}

// runCleanup runs the cleanup loop
func (c *AutoCleaner) runCleanup() {
	for {
		select {
		case <-c.ticker.C:
			c.CleanAll()
		case <-c.stopCleaner:
			return
		}
	}
}

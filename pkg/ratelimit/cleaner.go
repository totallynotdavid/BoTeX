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
	return &AutoCleaner{
		mu:          sync.Mutex{},
		cleanables:  make([]Cleanable, 0),
		ticker:      time.NewTicker(interval),
		stopCleaner: make(chan struct{}),
		isRunning:   false,
	}
}

func (c *AutoCleaner) Register(cleanable Cleanable) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanables = append(c.cleanables, cleanable)
	if !c.isRunning {
		go c.runCleanup()

		c.isRunning = true
	}
}

func (c *AutoCleaner) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunning {
		c.ticker.Stop()
		close(c.stopCleaner)
		c.isRunning = false
	}
}

func (c *AutoCleaner) CleanAll() {
	c.mu.Lock()
	cleanables := make([]Cleanable, len(c.cleanables))
	copy(cleanables, c.cleanables)
	c.mu.Unlock()

	for _, cleanable := range cleanables {
		cleanable.Cleanup()
	}
}

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

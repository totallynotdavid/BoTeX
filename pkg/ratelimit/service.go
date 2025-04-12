package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"botex/pkg/logger"
	"botex/pkg/message"
	"go.mau.fi/whatsmeow/types"
)

var (
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
	ErrServiceNotRunning = errors.New("rate limit service not running")
)

type RateLimitError struct {
	User       types.JID
	ResetAfter time.Duration
	Notify     bool
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded for %s (reset in %v)", e.User, e.ResetAfter)
}

type RateLimitService struct {
	limiter  *Limiter
	notifier *Notifier
	cleaner  *AutoCleaner
	logger   *logger.Logger
	running  bool
}

func NewRateLimitService(
	limiter *Limiter,
	notifier *Notifier,
	cleaner *AutoCleaner,
	logger *logger.Logger,
) *RateLimitService {
	return &RateLimitService{
		limiter:  limiter,
		notifier: notifier,
		cleaner:  cleaner,
		logger:   logger,
		running:  false,
	}
}

func (s *RateLimitService) Start() error {
	if s.running {
		return nil
	}

	s.cleaner.Register(s.limiter)
	s.cleaner.Register(s.notifier)
	s.running = true

	return nil
}

func (s *RateLimitService) Stop() {
	if !s.running {
		return
	}
	s.cleaner.Stop()
	s.running = false
}

func (s *RateLimitService) Check(ctx context.Context, msg *message.Message) error {
	if !s.running {
		return ErrServiceNotRunning
	}

	result := s.limiter.Check(msg.Sender)
	if result.Allowed {
		s.notifier.Clear(msg.Sender)

		return nil
	}

	shouldNotify := s.notifier.ShouldNotify(msg.Sender)
	s.logger.Warn("Rate limit exceeded", map[string]interface{}{
		"sender":     msg.Sender,
		"resetAfter": result.ResetAfter,
		"notify":     shouldNotify,
	})

	return &RateLimitError{
		User:       msg.Sender,
		ResetAfter: result.ResetAfter,
		Notify:     shouldNotify,
	}
}

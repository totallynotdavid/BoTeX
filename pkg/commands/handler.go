package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"
	"botex/pkg/ratelimit"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	defaultCommandTimeout = 30 * time.Second
	concurrentLimitMsg    = "Too many concurrent requests. Please try again later."
)

var (
	ErrTooManyConcurrent   = errors.New("too many concurrent commands")
	ErrCommandNotFound     = errors.New("command not found")
	ErrInvalidCommandInput = errors.New("invalid command input")
)

type Command interface {
	Handle(ctx context.Context, msg *message.Message) error
	Name() string
	Info() CommandInfo
}

type CommandInfo struct {
	Description string
	Usage       string
	Examples    []string
}

type CommandRegistry struct {
	commands []Command
	logger   *logger.Logger
}

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make([]Command, 0),
		logger:   logger.NewLogger(logger.DEBUG),
	}
}

func (r *CommandRegistry) Register(cmd Command) {
	r.commands = append(r.commands, cmd)
}

type CommandHandler struct {
	client        *whatsmeow.Client
	commands      map[string]Command
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	rateService   *ratelimit.RateLimitService
	semaphore     chan struct{}
}

func NewCommandHandler(client *whatsmeow.Client, config *config.Config, registry *CommandRegistry) (*CommandHandler, error) {
	limiter := ratelimit.NewLimiter(
		config.RateLimit.Requests,
		config.RateLimit.Period,
	)
	notifier := ratelimit.NewNotifier(
		config.RateLimit.NotificationCooldown,
	)
	cleaner := ratelimit.NewAutoCleaner(
		config.RateLimit.CleanupInterval,
	)
	rateService := ratelimit.NewRateLimitService(
		limiter,
		notifier,
		cleaner,
		logger.NewLogger(logger.INFO),
	)

	if err := rateService.Start(); err != nil {
		return nil, fmt.Errorf("failed to start rate limiter: %w", err)
	}

	handler := &CommandHandler{
		client:        client,
		commands:      make(map[string]Command),
		config:        config,
		messageSender: message.NewMessageSender(client),
		logger:        logger.NewLogger(logger.INFO),
		rateService:   rateService,
		semaphore:     make(chan struct{}, config.MaxConcurrent),
	}

	for _, cmd := range registry.commands {
		handler.commands[cmd.Name()] = cmd
	}

	return handler, nil
}

func (h *CommandHandler) GetCommands() []Command {
	cmds := make([]Command, 0, len(h.commands))
	for _, cmd := range h.commands {
		cmds = append(cmds, cmd)
	}

	return cmds
}

func (h *CommandHandler) Close() {
	h.rateService.Stop()
}

func (h *CommandHandler) parseCommand(text string) (cmdName, args string, ok bool) {
	if !strings.HasPrefix(text, "!") {
		return
	}
	parts := strings.Fields(text[1:])
	if len(parts) == 0 {
		return
	}
	cmdName = parts[0]
	args = strings.Join(parts[1:], " ")
	ok = true

	return
}

func (h *CommandHandler) handleRateLimitError(ctx context.Context, msg *message.Message, err error) {
	var rateErr *ratelimit.RateLimitError
	if !errors.As(err, &rateErr) {
		h.logger.Error("Unexpected error type in rate limiting", map[string]interface{}{
			"error":  err.Error(),
			"sender": msg.Sender,
		})

		return
	}

	if err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️"); err != nil {
		h.logger.Error("Failed to send rate limit reaction", map[string]interface{}{
			"error":     err.Error(),
			"sender":    msg.Sender,
			"messageID": msg.MessageID,
		})
	}

	if rateErr.Notify {
		waitMsg := fmt.Sprintf("Too many requests. Please wait %d seconds.", int(rateErr.ResetAfter.Seconds()))
		if err := h.messageSender.SendText(ctx, msg.Recipient, waitMsg); err != nil {
			h.logger.Error("Failed to send rate limit wait message", map[string]interface{}{
				"error":     err.Error(),
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}
	}
}

func (h *CommandHandler) acquireSemaphore(ctx context.Context) (func(), error) {
	select {
	case h.semaphore <- struct{}{}:
		return func() { <-h.semaphore }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("failed to acquire semaphore: %w", ctx.Err())
	default:
		return nil, fmt.Errorf("%w: max concurrent %d", ErrTooManyConcurrent, h.config.MaxConcurrent)
	}
}

func (h *CommandHandler) executeCommand(ctx context.Context, cmdName string, msg *message.Message) error {
	cmd, exists := h.commands[cmdName]
	if !exists {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, cmdName)
	}

	if err := cmd.Handle(ctx, msg); err != nil {
		h.logger.Error("Command execution failed", map[string]interface{}{
			"command": cmdName,
			"sender":  msg.Sender,
			"error":   err.Error(),
		})

		if err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "❌"); err != nil {
			h.logger.Error("Failed to send error reaction", map[string]interface{}{
				"error":     err.Error(),
				"command":   cmdName,
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		return fmt.Errorf("command %q execution failed: %w", cmdName, err)
	}

	if err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "✅"); err != nil {
		h.logger.Error("Failed to send success reaction", map[string]interface{}{
			"error":     err.Error(),
			"command":   cmdName,
			"sender":    msg.Sender,
			"messageID": msg.MessageID,
		})
	}

	return nil
}

func (h *CommandHandler) HandleEvent(evt interface{}) {
	msgEvent, ok := evt.(*events.Message)
	if !ok {
		return
	}

	msg := message.NewMessage(msgEvent)
	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()

	cmdName, args, isCommand := h.parseCommand(msg.GetText())
	if !isCommand {
		return
	}

	if err := h.rateService.Check(ctx, msg); err != nil {
		h.handleRateLimitError(ctx, msg, err)

		return
	}

	release, err := h.acquireSemaphore(ctx)
	if err != nil {
		h.logger.Warn("Concurrency limit exceeded", map[string]interface{}{
			"sender": msg.Sender,
			"error":  err.Error(),
		})

		if err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️"); err != nil {
			h.logger.Warn("Failed to send concurrency reaction", map[string]interface{}{
				"error":     err.Error(),
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		if err := h.messageSender.SendText(ctx, msg.Recipient, concurrentLimitMsg); err != nil {
			h.logger.Warn("Failed to send concurrency message", map[string]interface{}{
				"error":     err.Error(),
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		return
	}
	defer release()

	msg.Text = args
	if err := h.executeCommand(ctx, cmdName, msg); err != nil {
		h.logger.Error("Command handling failed", map[string]interface{}{
			"command": cmdName,
			"sender":  msg.Sender,
			"error":   err.Error(),
		})
	}
}

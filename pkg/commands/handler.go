package commands

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"botex/pkg/auth"
	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"
	"botex/pkg/ratelimit"
	"botex/pkg/timing"
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

func NewCommandRegistry(loggerFactory *logger.LoggerFactory) *CommandRegistry {
	return &CommandRegistry{
		commands: make([]Command, 0),
		logger:   loggerFactory.GetLogger("command-registry"),
	}
}

func (r *CommandRegistry) Register(cmd Command) {
	r.commands = append(r.commands, cmd)
}

type CommandHandler struct {
	client         *whatsmeow.Client
	commands       map[string]Command
	config         *config.Config
	messageSender  *message.MessageSender
	logger         *logger.Logger
	rateService    *ratelimit.RateLimitService
	semaphore      chan struct{}
	timeTracker    *timing.Tracker
	authService    *auth.Service
	authMiddleware *auth.Middleware
}

func NewCommandHandler(client *whatsmeow.Client, config *config.Config, registry *CommandRegistry, loggerFactory *logger.LoggerFactory) (*CommandHandler, error) {
	cmdLogger := loggerFactory.GetLogger("command-handler")

	// Initialize auth service
	db, err := sql.Open("sqlite3", config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	authService, err := auth.NewService(db, loggerFactory)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}
	authMiddleware := auth.NewMiddleware(authService)

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
		cmdLogger,
	)

	if err := rateService.Start(); err != nil {
		return nil, fmt.Errorf("failed to start rate limiter: %w", err)
	}

	timeTracker := timing.NewTrackerFromConfig(config, loggerFactory.GetLogger("timing"))

	handler := &CommandHandler{
		client:         client,
		commands:       make(map[string]Command),
		config:         config,
		messageSender:  message.NewMessageSender(client),
		logger:         cmdLogger,
		rateService:    rateService,
		semaphore:      make(chan struct{}, config.MaxConcurrent),
		timeTracker:    timeTracker,
		authService:    authService,
		authMiddleware: authMiddleware,
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

	if err := h.timeTracker.TrackSubOperation(ctx, "handle_rate_limit", func(ctx context.Context) error {
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

		return nil
	}); err != nil {
		h.logger.Error("Failed to track rate limit handling", map[string]interface{}{
			"error": err.Error(),
		})
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

func (h *CommandHandler) sendErrorReaction(ctx context.Context, msg *message.Message, cmdName string) error {
	if err := h.timeTracker.TrackSubOperation(ctx, "send_error_reaction", func(ctx context.Context) error {
		if err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "❌"); err != nil {
			h.logger.Error("Failed to send error reaction", map[string]interface{}{
				"error":     err.Error(),
				"command":   cmdName,
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to track error reaction: %w", err)
	}

	return nil
}

func (h *CommandHandler) sendSuccessReaction(ctx context.Context, msg *message.Message, cmdName string) error {
	if err := h.timeTracker.TrackSubOperation(ctx, "send_success_reaction", func(ctx context.Context) error {
		if err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "✅"); err != nil {
			h.logger.Error("Failed to send success reaction", map[string]interface{}{
				"error":     err.Error(),
				"command":   cmdName,
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to track success reaction: %w", err)
	}

	return nil
}

func (h *CommandHandler) executeCommand(ctx context.Context, cmdName string, msg *message.Message) error {
	cmd, exists := h.commands[cmdName]
	if !exists {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, cmdName)
	}

	// Check if command requires permission
	if permCmd, ok := cmd.(interface{ RequiredPermission() string }); ok {
		permission := permCmd.RequiredPermission()
		if err := h.authMiddleware.RequirePermission(permission)(ctx, msg.Sender.String()); err != nil {
			h.logger.Warn("Permission denied", map[string]interface{}{
				"command":    cmdName,
				"sender":     msg.Sender,
				"permission": permission,
			})

			return fmt.Errorf("permission denied: %w", err)
		}
	}

	if err := h.timeTracker.TrackCommand(ctx, cmdName, func(ctx context.Context) error {
		if err := cmd.Handle(ctx, msg); err != nil {
			h.logger.Error("Command execution failed", map[string]interface{}{
				"command": cmdName,
				"sender":  msg.Sender,
				"error":   err.Error(),
			})

			if err := h.sendErrorReaction(ctx, msg, cmdName); err != nil {
				h.logger.Error("Failed to track error reaction", map[string]interface{}{
					"error": err.Error(),
				})
			}

			return fmt.Errorf("command %q execution failed: %w", cmdName, err)
		}

		if err := h.sendSuccessReaction(ctx, msg, cmdName); err != nil {
			h.logger.Error("Failed to track success reaction", map[string]interface{}{
				"error": err.Error(),
			})
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to track command execution: %w", err)
	}

	return nil
}

func (h *CommandHandler) handleConcurrencyLimit(ctx context.Context, msg *message.Message) error {
	if err := h.timeTracker.TrackSubOperation(ctx, "handle_concurrency_limit", func(ctx context.Context) error {
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

		return nil
	}); err != nil {
		return fmt.Errorf("failed to track concurrency limit handling: %w", err)
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

	if err := h.timeTracker.Track(ctx, "handle_message", timing.Basic, func(ctx context.Context) error {
		cmdName, args, isCommand := h.parseCommand(msg.GetText())
		if !isCommand {
			return nil
		}

		ctx = timing.WithOperation(ctx, "command:"+cmdName)

		var rateLimitErr error
		if err := h.timeTracker.TrackSubOperation(ctx, "check_rate_limit", func(ctx context.Context) error {
			rateLimitErr = h.rateService.Check(ctx, msg)

			return nil
		}); err != nil {
			h.logger.Error("Failed to track rate limit check", map[string]interface{}{
				"error": err.Error(),
			})
		}

		if rateLimitErr != nil {
			h.handleRateLimitError(ctx, msg, rateLimitErr)

			return nil
		}

		var release func()
		var semErr error
		if err := h.timeTracker.TrackSubOperation(ctx, "acquire_semaphore", func(ctx context.Context) error {
			release, semErr = h.acquireSemaphore(ctx)

			return nil
		}); err != nil {
			h.logger.Error("Failed to track semaphore acquisition", map[string]interface{}{
				"error": err.Error(),
			})
		}

		if semErr != nil {
			h.logger.Warn("Concurrency limit exceeded", map[string]interface{}{
				"sender": msg.Sender,
				"error":  semErr.Error(),
			})

			return h.handleConcurrencyLimit(ctx, msg)
		}
		defer release()

		msg.Text = args

		return h.executeCommand(ctx, cmdName, msg)
	}); err != nil {
		h.logger.Error("Failed to track message handling", map[string]interface{}{
			"error": err.Error(),
		})
	}
}

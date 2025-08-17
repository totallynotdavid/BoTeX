package commands

import (
	"context"
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
	ErrPermissionDenied    = errors.New("permission denied")
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

func NewCommandRegistry(loggerFactory *logger.Factory) *CommandRegistry {
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
	authService    auth.AuthService
	contextBuilder auth.ContextBuilder
}

func NewCommandHandler(client *whatsmeow.Client, cfg *config.Config, registry *CommandRegistry, loggerFactory *logger.Factory, authService auth.AuthService) (*CommandHandler, error) {
	cmdLogger := loggerFactory.GetLogger("command-handler")

	limiter := ratelimit.NewLimiter(
		cfg.RateLimit.Requests,
		cfg.RateLimit.Period,
	)
	notifier := ratelimit.NewNotifier(
		cfg.RateLimit.NotificationCooldown,
	)
	cleaner := ratelimit.NewAutoCleaner(
		cfg.RateLimit.CleanupInterval,
	)
	rateService := ratelimit.NewRateLimitService(
		limiter,
		notifier,
		cleaner,
		cmdLogger,
	)

	err := rateService.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start rate limiter: %w", err)
	}

	timeTracker := timing.NewTrackerFromConfig(cfg, loggerFactory.GetLogger("timing"))

	handler := &CommandHandler{
		client:         client,
		commands:       make(map[string]Command),
		config:         cfg,
		messageSender:  message.NewMessageSender(client),
		logger:         cmdLogger,
		rateService:    rateService,
		semaphore:      make(chan struct{}, cfg.MaxConcurrent),
		timeTracker:    timeTracker,
		authService:    authService,
		contextBuilder: auth.NewContextBuilder(authService),
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

func (h *CommandHandler) HandleEvent(evt interface{}) {
	msgEvent, ok := evt.(*events.Message)
	if !ok {
		return
	}

	msg := message.NewMessage(msgEvent)

	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()

	err := h.timeTracker.Track(ctx, "handle_message", timing.Basic, func(ctx context.Context) error {
		// Build message context with unified permission checking
		var msgContext *auth.MessageContext
		var buildErr error
		
		trackSubOpErr := h.timeTracker.TrackSubOperation(ctx, "build_context", func(ctx context.Context) error {
			msgContext, buildErr = h.contextBuilder.BuildContext(ctx, msg)
			return nil
		})
		if trackSubOpErr != nil {
			h.logger.Error("Failed to track context building", map[string]interface{}{
				"error": trackSubOpErr.Error(),
			})
		}
		
		if buildErr != nil {
			h.logger.Error("Failed to build message context", map[string]interface{}{
				"sender": msg.Sender,
				"error":  buildErr.Error(),
			})
			return fmt.Errorf("failed to build message context: %w", buildErr)
		}

		// Early return if not a command
		if !msgContext.IsCommand() {
			return nil
		}

		ctx = timing.WithOperation(ctx, "command:"+msgContext.Command)

		// Early return if permission denied - implement early return logic
		if !msgContext.IsAllowed() {
			h.logger.Info("Command permission denied - early return", map[string]interface{}{
				"command":  msgContext.Command,
				"sender":   msg.Sender,
				"reason":   msgContext.GetPermissionReason(),
				"user_rank": msgContext.GetUserRank(),
			})
			
			// Handle permission denied with user feedback
			return h.handlePermissionDeniedFromContext(ctx, msgContext)
		}

		var rateLimitErr error

		trackSubOpErr = h.timeTracker.TrackSubOperation(ctx, "check_rate_limit", func(ctx context.Context) error {
			rateLimitErr = h.rateService.Check(ctx, msg)
			return nil
		})
		if trackSubOpErr != nil {
			h.logger.Error("Failed to track rate limit check", map[string]interface{}{
				"error": trackSubOpErr.Error(),
			})
		}

		if rateLimitErr != nil {
			h.handleRateLimitError(ctx, msg, rateLimitErr)
			return nil
		}

		var (
			release func()
			semErr  error
		)

		trackSubOpErr = h.timeTracker.TrackSubOperation(ctx, "acquire_semaphore", func(ctx context.Context) error {
			release, semErr = h.acquireSemaphore(ctx)
			return nil
		})
		if trackSubOpErr != nil {
			h.logger.Error("Failed to track semaphore acquisition", map[string]interface{}{
				"error": trackSubOpErr.Error(),
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

		// Set command args for the command handler
		msg.Text = msgContext.GetCommandArgs()

		return h.executeCommandWithContext(ctx, msgContext)
	})
	if err != nil {
		h.logger.Error("Failed to track message handling", map[string]interface{}{
			"error": err.Error(),
		})
	}
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

	trackErr := h.timeTracker.TrackSubOperation(ctx, "handle_rate_limit", func(ctx context.Context) error {
		var err error

		err = h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
		if err != nil {
			h.logger.Error("Failed to send rate limit reaction", map[string]interface{}{
				"error":     err.Error(),
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		if rateErr.Notify {
			waitMsg := fmt.Sprintf("Too many requests. Please wait %d seconds.", int(rateErr.ResetAfter.Seconds()))

			err = h.messageSender.SendText(ctx, msg.Recipient, waitMsg)
			if err != nil {
				h.logger.Error("Failed to send rate limit wait message", map[string]interface{}{
					"error":     err.Error(),
					"sender":    msg.Sender,
					"messageID": msg.MessageID,
				})
			}
		}

		return nil
	})
	if trackErr != nil {
		h.logger.Error("Failed to track rate limit handling", map[string]interface{}{
			"error": trackErr.Error(),
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
	err := h.timeTracker.TrackSubOperation(ctx, "send_error_reaction", func(ctx context.Context) error {
		err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "❌")
		if err != nil {
			h.logger.Error("Failed to send error reaction", map[string]interface{}{
				"error":     err.Error(),
				"command":   cmdName,
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to track error reaction: %w", err)
	}

	return nil
}

func (h *CommandHandler) sendSuccessReaction(ctx context.Context, msg *message.Message, cmdName string) error {
	err := h.timeTracker.TrackSubOperation(ctx, "send_success_reaction", func(ctx context.Context) error {
		err := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "✅")
		if err != nil {
			h.logger.Error("Failed to send success reaction", map[string]interface{}{
				"error":     err.Error(),
				"command":   cmdName,
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to track success reaction: %w", err)
	}

	return nil
}

func (h *CommandHandler) executeCommandWithContext(ctx context.Context, msgContext *auth.MessageContext) error {
	cmd, exists := h.commands[msgContext.Command]
	if !exists {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, msgContext.Command)
	}

	err := h.timeTracker.TrackCommand(ctx, msgContext.Command, func(ctx context.Context) error {
		// Permission already checked in context building - no need to check again
		// This implements the single permission check requirement
		
		err := cmd.Handle(ctx, msgContext.Message)
		if err != nil {
			h.logger.Error("Command execution failed", map[string]interface{}{
				"command": msgContext.Command,
				"sender":  msgContext.Message.Sender,
				"error":   err.Error(),
			})

			trackErr := h.sendErrorReaction(ctx, msgContext.Message, msgContext.Command)
			if trackErr != nil {
				h.logger.Error("Failed to track error reaction", map[string]interface{}{
					"error": trackErr.Error(),
				})
			}

			return fmt.Errorf("command %q execution failed: %w", msgContext.Command, err)
		}

		err = h.sendSuccessReaction(ctx, msgContext.Message, msgContext.Command)
		if err != nil {
			h.logger.Error("Failed to track success reaction", map[string]interface{}{
				"error": err.Error(),
			})
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to track command execution: %w", err)
	}

	return nil
}

func (h *CommandHandler) handleConcurrencyLimit(ctx context.Context, msg *message.Message) error {
	err := h.timeTracker.TrackSubOperation(ctx, "handle_concurrency_limit", func(ctx context.Context) error {
		var err error

		err = h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
		if err != nil {
			h.logger.Warn("Failed to send concurrency reaction", map[string]interface{}{
				"error":     err.Error(),
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		err = h.messageSender.SendText(ctx, msg.Recipient, concurrentLimitMsg)
		if err != nil {
			h.logger.Warn("Failed to send concurrency message", map[string]interface{}{
				"error":     err.Error(),
				"sender":    msg.Sender,
				"messageID": msg.MessageID,
			})
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to track concurrency limit handling: %w", err)
	}

	return nil
}

func (h *CommandHandler) handlePermissionDeniedFromContext(ctx context.Context, msgContext *auth.MessageContext) error {
	err := h.timeTracker.TrackSubOperation(ctx, "handle_permission_denied", func(ctx context.Context) error {
		var err error

		// Send permission denied reaction
		err = h.messageSender.SendReaction(ctx, msgContext.Message.Recipient, msgContext.Message.MessageID, "🚫")
		if err != nil {
			h.logger.Warn("Failed to send permission denied reaction", map[string]interface{}{
				"error":     err.Error(),
				"command":   msgContext.Command,
				"sender":    msgContext.Message.Sender,
				"messageID": msgContext.Message.MessageID,
			})
		}

		// Create user-friendly permission denied message using simplified error handling
		permissionMsg := h.createPermissionDeniedMessage(msgContext)

		// Send permission denied message
		err = h.messageSender.SendText(ctx, msgContext.Message.Recipient, permissionMsg)
		if err != nil {
			h.logger.Warn("Failed to send permission denied message", map[string]interface{}{
				"error":     err.Error(),
				"command":   msgContext.Command,
				"sender":    msgContext.Message.Sender,
				"messageID": msgContext.Message.MessageID,
			})
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to track permission denied handling: %w", err)
	}

	return ErrPermissionDenied
}

func (h *CommandHandler) createPermissionDeniedMessage(msgContext *auth.MessageContext) string {
	reason := msgContext.GetPermissionReason()
	cmdName := msgContext.Command
	userRank := msgContext.GetUserRank()
	isGroup := msgContext.IsGroupMessage()

	// Use simplified error handling based on common error patterns
	if isGroup {
		if strings.Contains(reason, "user not registered") || strings.Contains(reason, "user not found") {
			return "You need to be registered to use bot commands in this group. Please contact an administrator."
		} else if strings.Contains(reason, "group not registered") {
			return "This group is not registered for bot usage. Please contact an administrator to register the group."
		} else if strings.Contains(reason, "insufficient rank") || strings.Contains(reason, "insufficient permissions") {
			if userRank != "" {
				return fmt.Sprintf("You don't have sufficient permissions to use the '%s' command. Your rank: %s", cmdName, userRank)
			}
			return fmt.Sprintf("You don't have sufficient permissions to use the '%s' command.", cmdName)
		} else if strings.Contains(reason, "admin required") || strings.Contains(reason, "whatsapp admin") {
			return fmt.Sprintf("The '%s' command requires WhatsApp admin privileges in this group.", cmdName)
		} else if strings.Contains(reason, "command not allowed") || strings.Contains(reason, "invalid command") {
			return fmt.Sprintf("The '%s' command is not available for your rank.", cmdName)
		} else {
			return fmt.Sprintf("You don't have permission to use the '%s' command: %s", cmdName, reason)
		}
	} else {
		if strings.Contains(reason, "user not registered") || strings.Contains(reason, "user not found") {
			return "You need to be registered to use bot commands. Please contact an administrator."
		} else if strings.Contains(reason, "insufficient rank") || strings.Contains(reason, "insufficient permissions") {
			return fmt.Sprintf("You don't have sufficient permissions to use the '%s' command.", cmdName)
		} else if strings.Contains(reason, "command not allowed") || strings.Contains(reason, "invalid command") {
			return fmt.Sprintf("The '%s' command is not available for your rank.", cmdName)
		} else {
			return fmt.Sprintf("You don't have permission to use the '%s' command: %s", cmdName, reason)
		}
	}
}

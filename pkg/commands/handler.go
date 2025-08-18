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
	client        *whatsmeow.Client
	commands      map[string]Command
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	rateService   *ratelimit.RateLimitService
	semaphore     chan struct{}
	timeTracker   *timing.Tracker
	authService   auth.Auth
}

func NewCommandHandler(client *whatsmeow.Client, cfg *config.Config, registry *CommandRegistry, loggerFactory *logger.Factory, authService auth.Auth) (*CommandHandler, error) {
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
		client:        client,
		commands:      make(map[string]Command),
		config:        cfg,
		messageSender: message.NewMessageSender(client),
		logger:        cmdLogger,
		rateService:   rateService,
		semaphore:     make(chan struct{}, cfg.MaxConcurrent),
		timeTracker:   timeTracker,
		authService:   authService,
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
	msgEvent, isMessage := evt.(*events.Message)
	if !isMessage {
		return
	}

	msg := message.NewMessage(msgEvent)

	command, hasCommand := h.extractCommand(msg)
	if !hasCommand {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()

	if !h.checkPermission(ctx, msg, command) {
		return
	}

	h.processCommand(ctx, msg, command)
}

func (h *CommandHandler) extractCommand(msg *message.Message) (string, bool) {
	text := msg.GetText()
	if !strings.HasPrefix(text, "!") {
		return "", false
	}

	parts := strings.Fields(text[1:])
	if len(parts) == 0 {
		return "", false
	}

	if len(parts) > 1 {
		msg.Text = strings.Join(parts[1:], " ")
	} else {
		msg.Text = ""
	}

	return parts[0], true
}

func (h *CommandHandler) checkPermission(ctx context.Context, msg *message.Message, command string) bool {
	userID := msg.Sender.String()

	groupID := ""
	if msg.IsGroup {
		groupID = msg.GroupID.String()
	}

	permissionResult, err := h.authService.CheckPermission(ctx, userID, groupID, command)
	if err != nil {
		h.logger.Error("Permission check failed", map[string]interface{}{
			"command":  command,
			"sender":   userID,
			"group_id": groupID,
			"error":    err.Error(),
		})

		return false
	}

	if !permissionResult.Allowed {
		h.logger.Info("Command permission denied", map[string]interface{}{
			"command":   command,
			"sender":    userID,
			"reason":    permissionResult.Reason,
			"user_rank": permissionResult.UserRank,
		})
		h.handlePermissionDenied(ctx, msg, command, permissionResult)

		return false
	}

	return true
}

func (h *CommandHandler) processCommand(ctx context.Context, msg *message.Message, command string) {
	err := h.timeTracker.Track(ctx, "handle_command", timing.Basic, func(ctx context.Context) error {
		rateLimitErr := h.rateService.Check(ctx, msg)
		if rateLimitErr != nil {
			h.handleRateLimitError(ctx, msg, rateLimitErr)

			return nil
		}

		release, semErr := h.acquireSemaphore(ctx)
		if semErr != nil {
			h.logger.Warn("Concurrency limit exceeded", map[string]interface{}{
				"sender": msg.Sender,
			})
			h.handleConcurrencyLimit(ctx, msg)

			return nil
		}
		defer release()

		return h.executeCommand(ctx, msg, command)
	})
	if err != nil {
		h.logger.Error("Failed to track command handling", map[string]interface{}{
			"command": command,
			"sender":  msg.Sender,
			"error":   err.Error(),
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

	reactionErr := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
	if reactionErr != nil {
		h.logger.Error("Failed to send rate limit reaction", map[string]interface{}{"error": reactionErr.Error()})
	}

	if rateErr.Notify {
		waitMsg := fmt.Sprintf("Too many requests. Please wait %d seconds.", int(rateErr.ResetAfter.Seconds()))

		textErr := h.messageSender.SendText(ctx, msg.Recipient, waitMsg)
		if textErr != nil {
			h.logger.Error("Failed to send rate limit message", map[string]interface{}{"error": textErr.Error()})
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

func (h *CommandHandler) executeCommand(ctx context.Context, msg *message.Message, command string) error {
	cmd, exists := h.commands[command]
	if !exists {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, command)
	}

	err := h.timeTracker.TrackCommand(ctx, command, func(ctx context.Context) error {
		err := cmd.Handle(ctx, msg)
		if err != nil {
			h.logger.Error("Command execution failed", map[string]interface{}{
				"command": command,
				"sender":  msg.Sender,
				"error":   err.Error(),
			})

			reactionErr := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "❌")
			if reactionErr != nil {
				h.logger.Error("Failed to send error reaction", map[string]interface{}{"error": reactionErr.Error()})
			}

			return fmt.Errorf("command %q execution failed: %w", command, err)
		}

		reactionErr := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "✅")
		if reactionErr != nil {
			h.logger.Error("Failed to send success reaction", map[string]interface{}{"error": reactionErr.Error()})
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to track command execution: %w", err)
	}

	return nil
}

func (h *CommandHandler) handleConcurrencyLimit(ctx context.Context, msg *message.Message) {
	reactionErr := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
	if reactionErr != nil {
		h.logger.Error("Failed to send concurrency limit reaction", map[string]interface{}{"error": reactionErr.Error()})
	}

	textErr := h.messageSender.SendText(ctx, msg.Recipient, concurrentLimitMsg)
	if textErr != nil {
		h.logger.Error("Failed to send concurrency limit message", map[string]interface{}{"error": textErr.Error()})
	}
}

func (h *CommandHandler) handlePermissionDenied(ctx context.Context, msg *message.Message, command string, result *auth.PermissionResult) {
	reactionErr := h.messageSender.SendReaction(ctx, msg.Recipient, msg.MessageID, "🚫")
	if reactionErr != nil {
		h.logger.Error("Failed to send permission denied reaction", map[string]interface{}{"error": reactionErr.Error()})
	}

	permissionMsg := h.createPermissionDeniedMessage(msg.IsGroup, command, result.Reason)

	textErr := h.messageSender.SendText(ctx, msg.Recipient, permissionMsg)
	if textErr != nil {
		h.logger.Error("Failed to send permission denied message", map[string]interface{}{"error": textErr.Error()})
	}
}

func (h *CommandHandler) createPermissionDeniedMessage(isGroup bool, command, reason string) string {
	switch reason {
	case "User not registered":
		if isGroup {
			return "You must be a registered user to use commands in this group. Please contact an admin."
		}

		return "You must be a registered user to use commands. Please contact an admin."
	case "Group not registered":
		return "This group is not registered for bot usage. Please contact an admin."
	case "Command not allowed for your rank":
		return fmt.Sprintf("The command `!%s` is not available for your rank.", command)
	default:
		return fmt.Sprintf("You do not have permission to use the `!%s` command.", command)
	}
}

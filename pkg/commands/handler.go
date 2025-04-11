package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"
	"botex/pkg/ratelimit"
)

var (
	ErrRateLimitExceeded   = errors.New("rate limit exceeded")
	ErrTooManyConcurrent   = errors.New("too many concurrent commands")
	ErrCommandNotFound     = errors.New("command not found")
	ErrInvalidCommandInput = errors.New("invalid command input")
)

type CommandInfo struct {
	BriefDescription string
	Description      string
	Usage            string
	Parameters       []string
	Examples         []string
	Notes            []string
}

// Command is an interface that all commands must implement
type Command interface {
	Handle(ctx context.Context, msg *message.Message) error
	Name() string
	Info() CommandInfo
}

// CommandHandler processes incoming messages and routes them to command implementations
type CommandHandler struct {
	client        *whatsmeow.Client
	commands      map[string]Command
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	rateManager   *ratelimit.Manager
	semaphore     chan struct{}
}

func NewCommandHandler(client *whatsmeow.Client, config *config.Config) *CommandHandler {
	handler := &CommandHandler{
		client:        client,
		commands:      make(map[string]Command),
		config:        config,
		messageSender: message.NewMessageSender(client),
		logger:        logger.NewLogger(logger.INFO),
		rateManager:   ratelimit.NewManager(config.RateLimit.Requests, config.RateLimit.Period),
		semaphore:     make(chan struct{}, config.MaxConcurrent),
	}

	return handler
}

func (h *CommandHandler) Close() {
	h.rateManager.Stop()
}

func (h *CommandHandler) RegisterCommand(cmd Command) {
	h.commands[cmd.Name()] = cmd
}

// RegisterDefaultCommands registers the standard set of commands
func (h *CommandHandler) RegisterDefaultCommands() {
	latexCmd := NewLaTeXCommand(h.client, h.config)

	// Register all commands
	h.RegisterCommand(latexCmd)
	h.RegisterCommand(NewHelpCommand(h.client, h.config, []Command{latexCmd}))
}

func (h *CommandHandler) sendReaction(ctx context.Context, recipient types.JID, messageID string, emoji string) error {
	err := h.messageSender.SendReaction(ctx, recipient, messageID, emoji)
	if err != nil {
		h.logger.Error("Failed to send reaction", map[string]interface{}{
			"recipient": recipient,
			"emoji":     emoji,
			"error":     err.Error(),
		})
	}
	return err
}

func (h *CommandHandler) sendText(ctx context.Context, recipient types.JID, text string) error {
	err := h.messageSender.SendText(ctx, recipient, text)
	if err != nil {
		h.logger.Error("Failed to send text message", map[string]interface{}{
			"recipient": recipient,
			"error":     err.Error(),
		})
	}
	return err
}

// Extracts the command name and arguments from the message text
func (h *CommandHandler) parseCommand(text string) (string, string, bool) {
	if !strings.HasPrefix(text, "!") {
		return "", "", false
	}

	// Remove the ! prefix and split into parts
	parts := strings.Fields(text[1:])
	if len(parts) == 0 {
		return "", "", false
	}

	commandName := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}

	return commandName, args, true
}

// Checks if a user is rate limited and sends notifications if needed
func (h *CommandHandler) handleRateLimit(ctx context.Context, msg *message.Message) error {
	result := h.rateManager.Limiter.Check(msg.Sender)

	if !result.Allowed {
		h.logger.Warn("Rate limit exceeded", map[string]interface{}{
			"sender":     msg.Sender,
			"resetAfter": result.ResetAfter,
		})

		// Only notify if we haven't notified this user recently
		if h.rateManager.Notifier.ShouldNotify(msg.Sender) {
			_ = h.sendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
			waitMsg := fmt.Sprintf("Rate limit exceeded. Please wait about %d seconds before sending more commands.",
				int(result.ResetAfter.Seconds()))
			_ = h.sendText(ctx, msg.Recipient, waitMsg)
		}
		return ErrRateLimitExceeded
	}

	h.rateManager.Notifier.ClearNotification(msg.Sender)
	return nil
}

func (h *CommandHandler) acquireSemaphore(ctx context.Context, msg *message.Message) (release func(), err error) {
	select {
	case h.semaphore <- struct{}{}:
		return func() { <-h.semaphore }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		h.logger.Warn("Too many concurrent commands", map[string]interface{}{
			"sender": msg.Sender,
		})

		_ = h.sendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
		_ = h.sendText(ctx, msg.Recipient, "Too many commands being processed. Please wait a moment.")

		return nil, ErrTooManyConcurrent
	}
}

func (h *CommandHandler) executeCommand(ctx context.Context, cmdName string, msg *message.Message) error {
	cmd, exists := h.commands[cmdName]
	if !exists {
		return ErrCommandNotFound
	}

	err := cmd.Handle(ctx, msg)
	if err != nil {
		h.logger.Error("Error handling command", map[string]interface{}{
			"command": cmdName,
			"sender":  msg.Sender,
			"error":   err.Error(),
		})

		_ = h.sendReaction(ctx, msg.Recipient, msg.MessageID, "❌")
		_ = h.sendText(ctx, msg.Recipient, fmt.Sprintf("Error executing command: %v", err))
		return err
	}

	h.logger.Info("Command executed successfully", map[string]interface{}{
		"command": cmdName,
		"sender":  msg.Sender,
	})

	_ = h.sendReaction(ctx, msg.Recipient, msg.MessageID, "✅")
	return nil
}

func (h *CommandHandler) HandleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		msg := message.NewMessage(v)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		h.logger.Debug("Received message", map[string]interface{}{
			"sender":    msg.Sender,
			"recipient": msg.Recipient,
			"is_group":  msg.IsGroup,
			"text":      msg.GetText(),
		})

		text := msg.GetText()
		commandName, args, hasCommand := h.parseCommand(text)

		// Skip if not a command
		if !hasCommand {
			return
		}

		// Check rate limit
		if err := h.handleRateLimit(ctx, msg); err != nil {
			return
		}

		// Acquire semaphore for concurrency control
		release, err := h.acquireSemaphore(ctx, msg)
		if err != nil {
			return
		}
		defer release()

		// Set the command arguments in the message
		msg.Text = args

		// Execute the command
		_ = h.executeCommand(ctx, commandName, msg)
	}
}

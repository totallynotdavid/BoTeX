package commands

import (
	"context"
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

type CommandInfo struct {
	BriefDescription string
	Description      string
	Usage            string
	Parameters       []string
	Examples         []string
	Notes            []string
}

type CommandHandler struct {
	client        *whatsmeow.Client
	commands      []Command
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	limiter       *ratelimit.Limiter
	semaphore     chan struct{}
	notifiedUsers map[types.JID]bool
}

// Command is an interface that all commands must implement
type Command interface {
	Handle(ctx context.Context, msg *message.Message) error
	Name() string
	Info() CommandInfo
}

func NewCommandHandler(client *whatsmeow.Client, config *config.Config) *CommandHandler {
	handler := &CommandHandler{
		client:        client,
		config:        config,
		messageSender: message.NewMessageSender(client),
		logger:        logger.NewLogger(logger.INFO),
		limiter:       ratelimit.NewLimiter(config.RateLimit.Requests, config.RateLimit.Period),
		semaphore:     make(chan struct{}, config.MaxConcurrent),
		notifiedUsers: make(map[types.JID]bool),
	}

	// Register all available commands
	latexCmd := NewLaTeXCommand(client, config)
	handler.commands = []Command{
		latexCmd,
		NewHelpCommand(client, config, []Command{latexCmd}),
	}

	return handler
}

func (h *CommandHandler) sendReaction(ctx context.Context, recipient types.JID, messageID string, emoji string) {
	if err := h.messageSender.SendReaction(ctx, recipient, messageID, emoji); err != nil {
		h.logger.Error("Failed to send reaction", map[string]interface{}{
			"recipient": recipient,
			"error":     err.Error(),
		})
	}
}

func (h *CommandHandler) sendText(ctx context.Context, recipient types.JID, text string) {
	if err := h.messageSender.SendText(ctx, recipient, text); err != nil {
		h.logger.Error("Failed to send text message", map[string]interface{}{
			"recipient": recipient,
			"error":     err.Error(),
		})
	}
}

func (h *CommandHandler) HandleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		msg := message.NewMessage(v)
		ctx := context.Background()

		h.logger.Debug("Received message", map[string]interface{}{
			"sender":    msg.Sender,
			"recipient": msg.Recipient,
			"is_group":  msg.IsGroup,
			"text":      msg.GetText(),
		})

		text := msg.GetText()
		hasCommand := false
		for _, cmd := range h.commands {
			if strings.HasPrefix(text, "!"+cmd.Name()) {
				hasCommand = true
				break
			}
		}

		// Check rate limit
		if hasCommand {
			if !h.limiter.Allow(msg.Sender) {
				h.logger.Warn("Rate limit exceeded", map[string]interface{}{
					"sender": msg.Sender,
				})

				// Only notify if we haven't notified this user recently
				if !h.notifiedUsers[msg.Sender] {
					h.sendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
					h.sendText(ctx, msg.Recipient, "Rate limit exceeded. Please wait a moment before sending more commands.")
					h.notifiedUsers[msg.Sender] = true

					// Clear the notification flag after the rate limit period
					go func() {
						time.Sleep(h.config.RateLimit.Period)
						h.notifiedUsers[msg.Sender] = false
					}()
				}
				return
			}

			// Reset notification flag if user is now allowed
			if h.notifiedUsers[msg.Sender] {
				h.notifiedUsers[msg.Sender] = false
			}
		}

		// Acquire semaphore
		select {
		case h.semaphore <- struct{}{}:
			defer func() { <-h.semaphore }()
		default:
			h.logger.Warn("Too many concurrent commands", map[string]interface{}{
				"sender": msg.Sender,
			})
			h.sendReaction(ctx, msg.Recipient, msg.MessageID, "⚠️")
			h.sendText(ctx, msg.Recipient, "Too many commands being processed. Please wait a moment.")
			return
		}

		for _, cmd := range h.commands {
			if !strings.HasPrefix(text, "!"+cmd.Name()) {
				continue
			}
			if err := cmd.Handle(ctx, msg); err != nil {
				h.logger.Error("Error handling command", map[string]interface{}{
					"command": cmd.Name(),
					"sender":  msg.Sender,
					"error":   err.Error(),
				})
				h.sendReaction(ctx, msg.Recipient, msg.MessageID, "❌")
				h.sendText(ctx, msg.Recipient, fmt.Sprintf("Error executing command: %v", err))
			} else {
				h.logger.Info("Command executed successfully", map[string]interface{}{
					"command": cmd.Name(),
					"sender":  msg.Sender,
				})
				h.sendReaction(ctx, msg.Recipient, msg.MessageID, "✅")
			}
		}
	}
}

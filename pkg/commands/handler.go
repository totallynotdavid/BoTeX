package commands

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"
	"botex/pkg/ratelimit"
)

type CommandHandler struct {
	client        *whatsmeow.Client
	commands      []Command
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	limiter       *ratelimit.Limiter
	semaphore     chan struct{}
}

// Command is an interface that all commands must implement
type Command interface {
	Handle(ctx context.Context, msg *message.Message) error
	Name() string
}

func NewCommandHandler(client *whatsmeow.Client, config *config.Config) *CommandHandler {
	handler := &CommandHandler{
		client:        client,
		config:        config,
		messageSender: message.NewMessageSender(client),
		logger:        logger.NewLogger(logger.INFO),
		limiter:       ratelimit.NewLimiter(config.RateLimit.Requests, config.RateLimit.Period),
		semaphore:     make(chan struct{}, config.MaxConcurrent),
	}

	// Register all available commands
	handler.commands = []Command{
		NewLaTeXCommand(client, config),
	}

	return handler
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

		// Check rate limiting
		if !h.limiter.Allow(msg.Sender) {
			h.logger.Warn("Rate limit exceeded", map[string]interface{}{
				"sender": msg.Sender,
			})
			return
		}

		// Acquire semaphore
		select {
		case h.semaphore <- struct{}{}:
			defer func() { <-h.semaphore }()
		default:
			h.logger.Warn("Too many concurrent commands", map[string]interface{}{
				"sender": msg.Sender,
			})
			return
		}

		text := msg.GetText()
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
			} else {
				h.logger.Info("Command executed successfully", map[string]interface{}{
					"command": cmd.Name(),
					"sender":  msg.Sender,
				})
			}
		}
	}
}

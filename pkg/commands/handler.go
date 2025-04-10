package commands

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"botex/pkg/config"
	"botex/pkg/message"
)

type CommandHandler struct {
	client   *whatsmeow.Client
	commands []Command
	config   *config.Config
}

// Command is an interface that all commands must implement
type Command interface {
	Handle(ctx context.Context, msg *message.Message) error
}

func NewCommandHandler(client *whatsmeow.Client, config *config.Config) *CommandHandler {
	handler := &CommandHandler{
		client: client,
		config: config,
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

		for _, cmd := range h.commands {
			if err := cmd.Handle(ctx, msg); err != nil {
				fmt.Printf("Error handling command: %v\n", err)
			}
		}
	}
}

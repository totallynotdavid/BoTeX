package commands

import (
	"context"
	"fmt"
	"strings"

	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"
	"go.mau.fi/whatsmeow"
)

const (
	helpHeader           = "*Available Commands*\n\n"
	helpFooter           = "\nUse `!help <command>` for detailed usage."
	commandNotFoundMsg   = "Command `%s` not found. Use `!help` to see available commands."
	commandDetailsHeader = "*%s Command*\n\n"
	usagePrefix          = "*Usage:* `%s`\n"
	examplesHeader       = "\n*Examples:*\n"
)

type HelpCommand struct {
	client        *whatsmeow.Client
	config        *config.Config
	messageSender *message.MessageSender
	handler       *CommandHandler
	logger        *logger.Logger
}

func NewHelpCommand(client *whatsmeow.Client, cfg *config.Config, loggerFactory *logger.LoggerFactory) *HelpCommand {
	return &HelpCommand{
		client:        client,
		config:        cfg,
		messageSender: message.NewMessageSender(client),
		logger:        loggerFactory.GetLogger("help-command"),
	}
}

/*
SetHandler resolves a circular dependency in the command initialization sequence:

1. HelpCommand is created first with a nil handler so it can be registered in CommandRegistry.
2. CommandHandler is then created with the registry that already contains HelpCommand.
3. HelpCommand requires CommandHandler to implement generateGeneralHelp() and generateCommandHelp().

Without this method, we'd have a deadlock: HelpCommand depends on CommandHandler,
but CommandHandler also needs HelpCommand in the registry.
*/
func (hc *HelpCommand) SetHandler(handler *CommandHandler) {
	hc.handler = handler
}

func (hc *HelpCommand) Name() string {
	return "help"
}

func (hc *HelpCommand) Info() CommandInfo {
	return CommandInfo{
		Description: "Show available commands and their usage",
		Usage:       "!help [command]",
		Examples:    []string{"!help", "!help latex"},
	}
}

func (hc *HelpCommand) Handle(ctx context.Context, msg *message.Message) error {
	if err := hc.handler.timeTracker.TrackCommand(ctx, "help", func(ctx context.Context) error {
		args := strings.TrimSpace(msg.Text)
		var helpText string

		if args == "" {
			helpText = hc.generateGeneralHelp()
		} else {
			cmdName := strings.Split(args, " ")[0]
			var found bool
			helpText, found = hc.generateCommandHelp(cmdName)
			if !found {
				helpText = fmt.Sprintf(commandNotFoundMsg, cmdName)
			}
		}

		return hc.sendHelpResponse(ctx, msg, helpText)
	}); err != nil {
		return fmt.Errorf("failed to track help command execution: %w", err)
	}

	return nil
}

func (hc *HelpCommand) generateGeneralHelp() string {
	var builder strings.Builder
	builder.WriteString(helpHeader)
	for _, cmd := range hc.handler.GetCommands() {
		if cmd.Name() != "help" {
			builder.WriteString(fmt.Sprintf("• *%s* - %s\n", cmd.Name(), cmd.Info().Description))
		}
	}
	builder.WriteString(helpFooter)

	return builder.String()
}

func (hc *HelpCommand) generateCommandHelp(cmdName string) (string, bool) {
	cmd, exists := hc.handler.commands[cmdName]
	if !exists {
		return "", false
	}

	return hc.buildCommandDetails(cmd), true
}

func (hc *HelpCommand) buildCommandDetails(cmd Command) string {
	var builder strings.Builder
	info := cmd.Info()
	builder.WriteString(fmt.Sprintf(commandDetailsHeader, cmd.Name()))
	builder.WriteString(info.Description + "\n\n")
	builder.WriteString(fmt.Sprintf(usagePrefix, info.Usage))

	if len(info.Examples) > 0 {
		builder.WriteString(examplesHeader)
		for _, ex := range info.Examples {
			builder.WriteString(fmt.Sprintf("`%s`\n", ex))
		}
	}

	return builder.String()
}

func (hc *HelpCommand) sendHelpResponse(ctx context.Context, msg *message.Message, helpText string) error {
	if err := hc.messageSender.SendText(ctx, msg.Recipient, helpText); err != nil {
		hc.logger.Error("Failed to send help response", map[string]interface{}{
			"recipient": msg.Recipient,
			"error":     err.Error(),
		})

		return fmt.Errorf("failed to send help message: %w", err)
	}

	return nil
}

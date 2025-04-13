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
	config        *config.Config
	messageSender *message.MessageSender
	commands      []Command
	logger        *logger.Logger
}

func NewHelpCommand(client *whatsmeow.Client, config *config.Config, commands []Command) *HelpCommand {
	return &HelpCommand{
		config:        config,
		messageSender: message.NewMessageSender(client),
		commands:      commands,
		logger:        logger.NewLogger(logger.INFO),
	}
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
}

func (hc *HelpCommand) generateGeneralHelp() string {
	var builder strings.Builder
	builder.WriteString(helpHeader)

	for _, cmd := range hc.commands {
		builder.WriteString(fmt.Sprintf("• *%s* - %s\n", cmd.Name(), cmd.Info().Description))
	}

	builder.WriteString(helpFooter)

	return builder.String()
}

func (hc *HelpCommand) generateCommandHelp(cmdName string) (string, bool) {
	for _, cmd := range hc.commands {
		if cmd.Name() == cmdName {
			return hc.buildCommandDetails(cmd), true
		}
	}

	return "", false
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

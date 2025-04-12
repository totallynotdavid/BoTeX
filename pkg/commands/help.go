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
	var helpText strings.Builder

	if args == "" {
		helpText.WriteString("*Available Commands*\n\n")
		for _, cmd := range hc.commands {
			helpText.WriteString(fmt.Sprintf("• *%s* - %s\n", cmd.Name(), cmd.Info().Description))
		}
		helpText.WriteString("\nUse `!help <command>` for detailed usage.")
	} else {
		cmdName := strings.Split(args, " ")[0]
		var found bool
		for _, cmd := range hc.commands {
			if cmd.Name() == cmdName {
				info := cmd.Info()
				helpText.WriteString(fmt.Sprintf("*%s Command*\n\n", cmdName))
				helpText.WriteString(info.Description + "\n\n")
				helpText.WriteString(fmt.Sprintf("*Usage:* `%s`\n", info.Usage))

				if len(info.Examples) > 0 {
					helpText.WriteString("\n*Examples:*\n")
					for _, ex := range info.Examples {
						helpText.WriteString(fmt.Sprintf("`%s`\n", ex))
					}
				}
				found = true

				break
			}
		}
		if !found {
			helpText.WriteString(fmt.Sprintf("Command `%s` not found. Use `!help` to see available commands.", cmdName))
		}
	}

	return hc.messageSender.SendText(ctx, msg.Recipient, helpText.String())
}

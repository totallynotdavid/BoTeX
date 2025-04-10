package commands

import (
	"context"
	"fmt"
	"strings"

	"botex/pkg/config"
	"botex/pkg/message"

	"go.mau.fi/whatsmeow"
)

// HelpCommand displays information about all available commands
// Usage: !help
// This command shows a list of all available commands and their usage.
type HelpCommand struct {
	config        *config.Config
	messageSender *message.MessageSender
	commands      []Command
}

func NewHelpCommand(client *whatsmeow.Client, config *config.Config, commands []Command) *HelpCommand {
	return &HelpCommand{
		config:        config,
		messageSender: message.NewMessageSender(client),
		commands:      commands,
	}
}

func (hc *HelpCommand) Name() string {
	return "help"
}

func (hc *HelpCommand) Help() string {
	return "Displays information about all available commands.\n" +
		"Usage: !help"
}

func (hc *HelpCommand) Info() CommandInfo {
	return CommandInfo{
		BriefDescription: "Show available commands or get detailed help",
		Description:      "Displays information about all available commands or detailed help for a specific command.",
		Usage:            "!help [command]",
		Examples: []string{
			"!help",
			"!help latex",
		},
		Notes: []string{
			"Use without arguments to see all available commands",
			"Specify a command name to see detailed help for that command",
		},
	}
}

func (hc *HelpCommand) formatBriefHelp(cmd Command) string {
	info := cmd.Info()
	return fmt.Sprintf("• *%s*: %s", cmd.Name(), info.BriefDescription)
}

func (hc *HelpCommand) formatHelp(cmd Command) string {
	info := cmd.Info()
	var builder strings.Builder

	// Command name and description
	builder.WriteString(fmt.Sprintf("*%s*\n", cmd.Name()))
	builder.WriteString(fmt.Sprintf("%s\n\n", info.Description))

	// Usage
	builder.WriteString("*Usage:*\n")
	builder.WriteString(fmt.Sprintf("`%s`\n\n", info.Usage))

	// Parameters
	if len(info.Parameters) > 0 {
		builder.WriteString("*Parameters:*\n")
		for _, param := range info.Parameters {
			builder.WriteString(fmt.Sprintf("• %s\n", param))
		}
		builder.WriteString("\n")
	}

	// Examples
	if len(info.Examples) > 0 {
		builder.WriteString("*Examples:*\n")
		for _, example := range info.Examples {
			builder.WriteString(fmt.Sprintf("`%s`\n", example))
		}
		builder.WriteString("\n")
	}

	// Notes
	if len(info.Notes) > 0 {
		builder.WriteString("*Notes:*\n")
		for _, note := range info.Notes {
			builder.WriteString(fmt.Sprintf("> %s\n", note))
		}
	}

	return builder.String()
}

func (hc *HelpCommand) Handle(ctx context.Context, msg *message.Message) error {
	text := msg.GetText()
	if !strings.HasPrefix(text, "!help") {
		return nil
	}

	parts := strings.Fields(text)
	var helpText strings.Builder

	if len(parts) == 1 {
		helpText.WriteString("*Available Commands*\n\n")
		for _, cmd := range hc.commands {
			helpText.WriteString(hc.formatBriefHelp(cmd))
			helpText.WriteString("\n")
		}
		helpText.WriteString("\nUse `!help <command>` for detailed information about a specific command.")
	} else {
		cmdName := parts[1]
		var found bool
		for _, cmd := range hc.commands {
			if cmd.Name() == cmdName {
				helpText.WriteString(hc.formatHelp(cmd))
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

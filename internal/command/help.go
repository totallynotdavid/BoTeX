package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// help lists the commands, or describes one.
type help struct {
	prefix string
	// listed are the commands "!help" lists, in the order they were given.
	listed []Command
	// all holds every command by name, help included, so "!help help" works.
	all map[string]Command
}

func (h *help) Name() string { return "help" }

func (h *help) Info() Info {
	return Info{
		Description: "Show available commands and their usage",
		Usage:       h.prefix + "help [command]",
		Examples:    []string{h.prefix + "help", h.prefix + "help " + h.firstListed()},
	}
}

func (h *help) Run(ctx context.Context, _ bot.Message, args string, chat *bot.Chat) error {
	text := h.list()

	fields := strings.Fields(args)
	if len(fields) > 0 {
		name := fields[0]

		cmd, found := h.all[name]
		if found {
			text = h.describe(cmd)
		} else {
			text = fmt.Sprintf("Command %#q not found. Use `%shelp` to see available commands.", name, h.prefix)
		}
	}

	err := chat.Send(ctx, text)
	if err != nil {
		return fmt.Errorf("send help: %w", err)
	}

	return nil
}

func (h *help) list() string {
	var out strings.Builder

	out.WriteString("*Available Commands*\n\n")

	for _, cmd := range h.listed {
		fmt.Fprintf(&out, "• *%s* - %s\n", cmd.Name(), cmd.Info().Description)
	}

	fmt.Fprintf(&out, "\nUse `%shelp <command>` for detailed usage.", h.prefix)

	return out.String()
}

func (h *help) describe(cmd Command) string {
	info := cmd.Info()

	var out strings.Builder

	fmt.Fprintf(&out, "*%s Command*\n\n%s\n\n*Usage:* `%s`\n", cmd.Name(), info.Description, info.Usage)

	if len(info.Examples) > 0 {
		out.WriteString("\n*Examples:*\n")

		for _, example := range info.Examples {
			fmt.Fprintf(&out, "`%s`\n", example)
		}
	}

	return out.String()
}

// firstListed names a command for help's own example, or help when there is
// none.
func (h *help) firstListed() string {
	if len(h.listed) == 0 {
		return "help"
	}

	return h.listed[0].Name()
}

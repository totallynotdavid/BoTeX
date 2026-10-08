package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/flow"
)

// hintWidth is the most runes of a user's last message that the list shows.
const hintWidth = 40

// handoffSubcommand lists the users the flow handed to a person, and clears a
// user once the person has taken over.
func handoffSubcommand() cli.Subcommand {
	return cli.Subcommand{
		Name:  "handoff",
		Usage: "list | clear <jid>",
		Run:   runHandoff,
	}
}

func runHandoff(ctx context.Context, operator cli.Operator, args []string) error {
	store, err := flow.NewStore(ctx, operator.Database)
	if err != nil {
		return fmt.Errorf("set up flow store: %w", err)
	}

	switch {
	case len(args) == 1 && args[0] == "list":
		return listHandoffs(ctx, store, operator)
	case len(args) == 2 && args[0] == "clear":
		return clearHandoff(ctx, store, operator, args[1])
	default:
		return fmt.Errorf("%w: want list or clear <jid>", cli.ErrUsage)
	}
}

func listHandoffs(ctx context.Context, store *flow.Store, operator cli.Operator) error {
	handoffs, err := store.Handoffs(ctx)
	if err != nil {
		return fmt.Errorf("list hand-offs: %w", err)
	}

	rows := [][]string{{"JID", "NAME", "NODE", "SINCE", "LAST MESSAGE"}}
	for _, handoff := range handoffs {
		rows = append(rows, []string{
			string(handoff.User), handoff.Name, handoff.Node,
			handoff.Since.Format(time.DateTime), shorten(handoff.LastMessage),
		})
	}

	err = operator.Table(rows...)
	if err != nil {
		return fmt.Errorf("write hand-offs: %w", err)
	}

	return nil
}

func clearHandoff(ctx context.Context, store *flow.Store, operator cli.Operator, operand string) error {
	jid, err := auth.ParseJID(operand)
	if err != nil {
		return fmt.Errorf("%w: %w", cli.ErrUsage, err)
	}

	err = store.ClearHandoff(ctx, bot.JID(jid))
	if err != nil {
		return fmt.Errorf("clear hand-off: %w", err)
	}

	_, err = fmt.Fprintf(operator.Out, "cleared %s\n", jid)
	if err != nil {
		return fmt.Errorf("write result: %w", err)
	}

	return nil
}

// shorten puts text on one line of at most hintWidth runes.
func shorten(text string) string {
	text = strings.Join(strings.Fields(text), " ")

	runes := []rune(text)
	if len(runes) <= hintWidth {
		return text
	}

	return string(runes[:hintWidth-1]) + "…"
}

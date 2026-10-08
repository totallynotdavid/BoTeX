package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
)

// adminLabel is the registered_by of everyone the operator registers.
const adminLabel = "cli"

var errChangeRank = errors.New("remove the user first to change the rank")

// userSubcommand registers the users of a bot that decides by rank. rank is
// the rank a user gets without --rank.
func userSubcommand(rank string) Subcommand {
	return Subcommand{
		Name:  "user",
		Usage: "add <jid> [--rank <rank>] | remove <jid> | list",
		Run: func(ctx context.Context, operator Operator, args []string) error {
			action, rest, err := choose(args, "add", "remove", "list")
			if err != nil {
				return err
			}

			switch action {
			case "add":
				return addUser(ctx, operator, rest, rank)
			case "remove":
				return removeUser(ctx, operator, rest)
			default:
				return listUsers(ctx, operator, rest)
			}
		},
	}
}

func addUser(ctx context.Context, operator Operator, args []string, defaultRank string) error {
	flags := flag.NewFlagSet("user add", flag.ContinueOnError)
	rank := flags.String("rank", defaultRank, "the rank the user gets")

	jid, err := jidOperand(flags, args)
	if err != nil {
		return err
	}

	err = operator.Users.RegisterUser(ctx, jid, *rank, adminLabel)
	if errors.Is(err, auth.ErrRankNotFound) {
		return fmt.Errorf("rank %q: %w", *rank, err)
	}

	if err != nil {
		return describeAuthError(err, jid)
	}

	return operator.say("added %s as %s\n", jid, *rank)
}

func removeUser(ctx context.Context, operator Operator, args []string) error {
	jid, err := jidOperand(flag.NewFlagSet("user remove", flag.ContinueOnError), args)
	if err != nil {
		return err
	}

	err = operator.Users.DeactivateUser(ctx, jid)
	if err != nil {
		return describeAuthError(err, jid)
	}

	return operator.say("removed %s\n", jid)
}

func listUsers(ctx context.Context, operator Operator, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: list takes no arguments", ErrUsage)
	}

	users, err := operator.Users.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}

	rows := [][]string{{"JID", "RANK", "SINCE", "BY"}}
	for _, user := range users {
		rows = append(rows, []string{user.ID, user.Rank, user.RegisteredAt.Format(time.DateOnly), user.RegisteredBy})
	}

	return operator.Table(rows...)
}

func groupSubcommand() Subcommand {
	return Subcommand{
		Name:  "group",
		Usage: "add <jid> | remove <jid> | list",
		Run: func(ctx context.Context, operator Operator, args []string) error {
			action, rest, err := choose(args, "add", "remove", "list")
			if err != nil {
				return err
			}

			switch action {
			case "add":
				return addGroup(ctx, operator, rest)
			case "remove":
				return removeGroup(ctx, operator, rest)
			default:
				return listGroups(ctx, operator, rest)
			}
		},
	}
}

func addGroup(ctx context.Context, operator Operator, args []string) error {
	jid, err := jidOperand(flag.NewFlagSet("group add", flag.ContinueOnError), args)
	if err != nil {
		return err
	}

	err = operator.Users.RegisterGroup(ctx, jid, adminLabel)
	if err != nil {
		return describeAuthError(err, jid)
	}

	return operator.say("added group %s\n", jid)
}

func removeGroup(ctx context.Context, operator Operator, args []string) error {
	jid, err := jidOperand(flag.NewFlagSet("group remove", flag.ContinueOnError), args)
	if err != nil {
		return err
	}

	err = operator.Users.DeactivateGroup(ctx, jid)
	if err != nil {
		return describeAuthError(err, jid)
	}

	return operator.say("removed group %s\n", jid)
}

func listGroups(ctx context.Context, operator Operator, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: list takes no arguments", ErrUsage)
	}

	groups, err := operator.Users.ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("list groups: %w", err)
	}

	rows := [][]string{{"JID", "SINCE", "BY"}}
	for _, group := range groups {
		rows = append(rows, []string{group.ID, group.RegisteredAt.Format(time.DateOnly), group.RegisteredBy})
	}

	return operator.Table(rows...)
}

func (o Operator) say(format string, args ...any) error {
	_, err := fmt.Fprintf(o.Out, format, args...)
	if err != nil {
		return fmt.Errorf("write result: %w", err)
	}

	return nil
}

func jidOperand(flags *flag.FlagSet, args []string) (string, error) {
	operand, err := parseOperand(flags, args)
	if err != nil {
		return "", err
	}

	if operand == "" {
		return "", fmt.Errorf("%w: a JID is required", ErrUsage)
	}

	jid, err := auth.ParseJID(operand)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUsage, err)
	}

	return jid, nil
}

// describeAuthError names the JID in a refusal from auth, and says how to get
// past it when there is a way.
func describeAuthError(err error, jid string) error {
	switch {
	case errors.Is(err, auth.ErrUserExists):
		return fmt.Errorf("%s: %w; %w", jid, err, errChangeRank)
	case errors.Is(err, auth.ErrInvalidJID):
		return fmt.Errorf("%w: %w", ErrUsage, err)
	case errors.Is(err, auth.ErrGroupExists), errors.Is(err, auth.ErrUserNotFound), errors.Is(err, auth.ErrGroupNotRegistered):
		return fmt.Errorf("%s: %w", jid, err)
	default:
		return err
	}
}

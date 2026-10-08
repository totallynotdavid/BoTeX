package cli

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/sqlite"
)

// ErrUsage is wrapped by the error of a subcommand run with arguments it does
// not accept. The command line answers it with the usage and exit status 2.
var ErrUsage = errors.New("usage")

// Subcommand is a task for the operator that a bot adds to the command line,
// beside run and pair. It works on the bot's database while the bot runs or
// stops.
type Subcommand struct {
	// Name is the word after the binary's name.
	Name string
	// Usage is what follows Name in the usage message, for example
	// "list | clear <jid>".
	Usage string
	// Run does the task. It writes its result to op.Out and wraps ErrUsage in
	// the error for arguments it does not accept.
	Run func(ctx context.Context, op Operator, args []string) error
}

// Operator is what a subcommand works with: the bot's open database, the
// users and ranks in it, and where the result goes.
type Operator struct {
	Database *sql.DB
	Users    *auth.Service
	Out      io.Writer
}

// Table writes rows to Out as aligned columns.
func (o Operator) Table(rows ...[]string) error {
	table := tabwriter.NewWriter(o.Out, 0, 0, tablePadding, ' ', 0)

	for _, row := range rows {
		_, err := fmt.Fprintln(table, strings.Join(row, "\t"))
		if err != nil {
			return fmt.Errorf("write table: %w", err)
		}
	}

	err := table.Flush()
	if err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	return nil
}

const tablePadding = 2

// subcommands are all the subcommands cmd has: the user and group commands
// for a bot that decides by rank, then the bot's own.
func subcommands(cmd Command) []Subcommand {
	var all []Subcommand

	if len(cmd.Ranks) > 0 {
		all = append(all, userSubcommand(cmd.Ranks[0].Name))
	}

	if cmd.Groups {
		all = append(all, groupSubcommand())
	}

	return append(all, cmd.Subcommands...)
}

func subcommandCommand(ctx context.Context, cmd Command, sub Subcommand, args []string, out io.Writer) int {
	cfg, _, err := setup(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmd.Name+":", err)

		return ExitFailure
	}

	err = runSubcommand(ctx, cfg, sub, args, out)

	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrUsage):
		fmt.Fprintf(os.Stderr, "%s %s: %v\nusage: %s %s %s\n", cmd.Name, sub.Name, err, cmd.Name, sub.Name, sub.Usage)

		return ExitUsage
	default:
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", cmd.Name, sub.Name, err)

		return ExitFailure
	}
}

func runSubcommand(ctx context.Context, cfg settings, sub Subcommand, args []string, out io.Writer) (err error) {
	database, err := sqlite.OpenExisting(ctx, cfg.shared.Store)
	if errors.Is(err, sqlite.ErrNoStore) {
		return fmt.Errorf("open store: %w; run the command in the bot's working directory or with its %s", err, config.KeyStore)
	}

	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	defer func() { err = errors.Join(err, database.Close()) }()

	users, err := auth.New(ctx, database, cfg.ranks...)
	if err != nil {
		return fmt.Errorf("set up auth: %w", err)
	}

	return sub.Run(ctx, Operator{Database: database, Users: users, Out: out}, args)
}

// parseOperand parses the arguments of a subcommand that takes one operand
// and flags, with the operand before or after the flags. It returns the
// operand, or an error wrapping ErrUsage.
func parseOperand(flags *flag.FlagSet, args []string) (string, error) {
	flags.SetOutput(io.Discard)

	var operand string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		operand, args = args[0], args[1:]
	}

	err := flags.Parse(args)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUsage, err)
	}

	rest := flags.Args()
	if operand == "" && len(rest) > 0 {
		operand, rest = rest[0], rest[1:]
	}

	if len(rest) > 0 {
		return "", fmt.Errorf("%w: unexpected argument %q", ErrUsage, rest[0])
	}

	return operand, nil
}

// choose returns the action in args[0] and its arguments when it is one of
// actions. Otherwise it returns an error wrapping ErrUsage.
func choose(args []string, actions ...string) (action string, rest []string, err error) {
	if len(args) == 0 || !slices.Contains(actions, args[0]) {
		return "", nil, fmt.Errorf("%w: want one of %s", ErrUsage, strings.Join(actions, ", "))
	}

	return args[0], args[1:], nil
}

// Package cli is the command line every bot binary shares. A binary describes
// its bot with a [Command] and calls [Main]:
//
//	<name> [run]          run the bot
//	<name> pair [--phone +<digits>]
//	                      link the bot to a WhatsApp account
//	<name> --repl [--owner]
//	                      chat with the bot through an in-memory transport
//
// The exit status is 0 after SIGINT or SIGTERM, 78 when the WhatsApp session
// ended and needs the operator, 1 for any other failure and 2 for bad usage.
package cli

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/config"
)

// Command is what makes one binary its bot. Everything else, from the
// subcommands to the store and the WhatsApp connection, is the same for all.
type Command struct {
	// Name is the binary's name, which its messages start with.
	Name string
	// Ranks are the ranks the bot's users hold besides owner.
	Ranks []auth.Rank
	// RateLimit is the bot's own default for the rate-limit settings, which the
	// BOTKIT_RATE_LIMIT_* keys still override. The zero value keeps the shared
	// default.
	RateLimit config.RateLimit
	// Configure reads the bot's own settings from env, together with the shared
	// ones, so one error names every bad key before anything is opened. It
	// returns the function that builds the bot once the store is open.
	Configure func(env *config.Env) Build
}

// Build makes the bot's app over the open database. users has the ranks of
// Command.Ranks.
type Build func(ctx context.Context, database *sql.DB, users *auth.Service, log *slog.Logger) (Built, error)

// Built is what a bot supplies to run.
type Built struct {
	App bot.App
	// Groups makes the bot answer messages sent in groups.
	Groups bool
	// Close releases what the app holds. Nil when it holds nothing.
	Close func() error
}

// settings is everything the bot reads from the environment.
type settings struct {
	shared config.Shared
	ranks  []auth.Rank
	build  Build
}

// readSettings reads the environment. It fails naming every bad key.
func readSettings(cmd Command, env *config.Env) (settings, error) {
	cfg := read(cmd, env)

	err := env.Err()
	if err != nil {
		return settings{}, fmt.Errorf("configuration: %w", err)
	}

	return cfg, nil
}

// read reads every setting of cmd from env. Callers inspect env.Err for
// malformed values.
func read(cmd Command, env *config.Env) settings {
	defaults := config.DefaultShared()
	if cmd.RateLimit != (config.RateLimit{}) {
		defaults.RateLimit = cmd.RateLimit
	}

	return settings{
		shared: env.Shared(defaults),
		ranks:  cmd.Ranks,
		build:  cmd.Configure(env),
	}
}

// Describe lists every setting cmd reads, shared ones included, with the
// default cmd uses for each. It reads an empty environment, where no setting
// can be malformed.
func Describe(cmd Command) []config.Entry {
	env := config.New(func(string) (string, bool) { return "", false })

	read(cmd, env)

	return env.Entries()
}

// Main runs cmd as the process: it parses the subcommand from os.Args, stops
// the bot on SIGINT and SIGTERM, and exits with the status of the subcommand.
func Main(cmd Command) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	status := execute(ctx, cmd, os.Args[1:])

	stop()
	os.Exit(status)
}

// execute runs the subcommand args name and returns the exit status.
func execute(ctx context.Context, cmd Command, args []string) int {
	if len(args) > 0 && args[0] == "--repl" {
		return replCommand(ctx, cmd, args[1:])
	}

	name, rest := "run", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, rest = args[0], args[1:]
	}

	switch name {
	case "run":
		return runCommand(ctx, cmd, rest)
	case "pair":
		return pairCommand(ctx, cmd, rest)
	default:
		fmt.Fprintf(os.Stderr, "%s: unknown command %q; want run or pair\n", cmd.Name, name)

		return ExitUsage
	}
}

func replCommand(ctx context.Context, cmd Command, args []string) int {
	flags := flag.NewFlagSet(cmd.Name+" --repl", flag.ContinueOnError)
	owner := flags.Bool("owner", false, "run as the configured owner")

	err := flags.Parse(args)
	if err != nil || flags.NArg() > 0 {
		return ExitUsage
	}

	cfg, log, err := setup(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmd.Name+" REPL:", err)

		return ExitFailure
	}

	err = repl(ctx, cfg, log, os.Stdin, os.Stdout, replOptions{owner: *owner})
	if err != nil {
		log.ErrorContext(ctx, cmd.Name+" REPL failed", "error", err)

		return ExitStatus(ctx, err)
	}

	return ExitOK
}

func runCommand(ctx context.Context, cmd Command, args []string) int {
	flags := flag.NewFlagSet(cmd.Name+" run", flag.ContinueOnError)

	err := flags.Parse(args)
	if err != nil || flags.NArg() > 0 {
		return ExitUsage
	}

	cfg, log, err := setup(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmd.Name+":", err)

		return ExitFailure
	}

	err = run(ctx, cfg, log, openWhatsApp)

	status := ExitStatus(ctx, err)
	if status == ExitFailure {
		log.ErrorContext(ctx, cmd.Name+" bot failed", "error", err)
	}

	return status
}

func pairCommand(ctx context.Context, cmd Command, args []string) int {
	flags := flag.NewFlagSet(cmd.Name+" pair", flag.ContinueOnError)
	phone := flags.String("phone", "", "link with a pairing code for this number, +<digits>, instead of a QR code")

	err := flags.Parse(args)
	if err != nil || flags.NArg() > 0 {
		return ExitUsage
	}

	cfg, log, err := setup(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmd.Name+":", err)

		return ExitFailure
	}

	err = pair(ctx, cfg, log, *phone)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmd.Name+" pair:", err)

		return ExitFailure
	}

	return ExitOK
}

// setup reads the environment and builds the one logger, which writes to
// stderr at BOTKIT_LOG_LEVEL.
func setup(cmd Command) (settings, *slog.Logger, error) {
	cfg, err := readSettings(cmd, config.FromEnviron())
	if err != nil {
		return settings{}, nil, err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.shared.LogLevel}))

	return cfg, log, nil
}

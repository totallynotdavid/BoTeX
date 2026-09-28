// Command latex is the WhatsApp bot that renders LaTeX equations.
//
//	latex [run]          run the bot
//	latex pair [--phone +<digits>]
//	                     link the bot to a WhatsApp account
//
// Its exit status is 0 after SIGINT or SIGTERM, 78 when the WhatsApp session
// ended and needs the operator, 1 for any other failure and 2 for bad usage.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp"
)

const exitUsage = 2

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	status := execute(ctx, os.Args[1:])

	stop()
	os.Exit(status)
}

// execute runs the subcommand args name and returns the exit status.
func execute(ctx context.Context, args []string) int {
	name, rest := "run", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, rest = args[0], args[1:]
	}

	switch name {
	case "run":
		return runCommand(ctx, rest)
	case "pair":
		return pairCommand(ctx, rest)
	default:
		fmt.Fprintf(os.Stderr, "latex: unknown command %q; want run or pair\n", name)

		return exitUsage
	}
}

func runCommand(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("latex run", flag.ContinueOnError)

	err := flags.Parse(args)
	if err != nil || flags.NArg() > 0 {
		return exitUsage
	}

	cfg, log, err := setup()
	if err != nil {
		fmt.Fprintln(os.Stderr, "latex:", err)

		return exitFailure
	}

	err = run(ctx, cfg, log, openWhatsApp)

	status := exitStatus(ctx, err)
	if status == exitFailure {
		log.ErrorContext(ctx, "latex bot failed", "error", err)
	}

	return status
}

func pairCommand(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("latex pair", flag.ContinueOnError)
	phone := flags.String("phone", "", "link with a pairing code for this number, +<digits>, instead of a QR code")

	err := flags.Parse(args)
	if err != nil || flags.NArg() > 0 {
		return exitUsage
	}

	cfg, log, err := setup()
	if err != nil {
		fmt.Fprintln(os.Stderr, "latex:", err)

		return exitFailure
	}

	err = pair(ctx, cfg, log, *phone)
	if err != nil {
		fmt.Fprintln(os.Stderr, "latex pair:", err)

		return exitFailure
	}

	return exitOK
}

func pair(ctx context.Context, cfg settings, log *slog.Logger, phone string) (err error) {
	database, err := sqlite.Open(ctx, cfg.shared.Store)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	defer func() {
		err = errors.Join(err, database.Close())
	}()

	return whatsapp.Pair(ctx, database, log, whatsapp.PairOptions{Phone: phone, In: os.Stdin, Out: os.Stdout}) //nolint:wrapcheck // Pair's errors already say what failed.
}

// setup reads the environment and builds the one logger, which writes to
// stderr at BOTKIT_LOG_LEVEL.
func setup() (settings, *slog.Logger, error) {
	cfg, err := readSettings(config.FromEnviron())
	if err != nil {
		return settings{}, nil, err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.shared.LogLevel}))

	return cfg, log, nil
}

//nolint:ireturn // openClient's contract is the interface, so tests can supply a fake.
func openWhatsApp(ctx context.Context, database *sql.DB, log *slog.Logger) (bot.Client, error) {
	client, err := whatsapp.Open(ctx, database, log)
	if err != nil {
		return nil, err //nolint:wrapcheck // run adds the context.
	}

	return client, nil
}

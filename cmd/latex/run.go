package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/command"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/latex"
	"github.com/totallynotdavid/botkit/internal/ratelimit"
	"github.com/totallynotdavid/botkit/internal/sqlite"
)

const (
	exitOK      = 0
	exitFailure = 1
	// exitConfig is EX_CONFIG from sysexits.h. The bot returns it when the
	// WhatsApp session is gone and only the operator can fix that, so a service
	// manager told to skip restarts on it does not loop.
	exitConfig = 78

	userRankLevel = 100
)

// settings is everything the bot reads from the environment.
type settings struct {
	shared config.Shared
	latex  latex.Config
}

// readSettings reads the environment. It fails naming every bad key.
func readSettings(env *config.Env) (settings, error) {
	cfg := settings{
		shared: env.Shared(config.DefaultShared()),
		latex:  latex.ConfigFromEnv(env),
	}

	err := env.Err()
	if err != nil {
		return settings{}, fmt.Errorf("configuration: %w", err)
	}

	return cfg, nil
}

// openClient returns the WhatsApp connection over database.
type openClient func(ctx context.Context, database *sql.DB, log *slog.Logger) (bot.Client, error)

// run builds the bot from cfg and runs it until ctx is done or the WhatsApp
// session ends.
func run(ctx context.Context, cfg settings, log *slog.Logger, open openClient) (err error) {
	database, err := sqlite.Open(ctx, cfg.shared.Store)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	defer func() {
		err = errors.Join(err, database.Close())
	}()

	authService, err := auth.New(ctx, database, auth.Rank{
		Name:        "user",
		Level:       userRankLevel,
		Commands:    []string{"help", "latex"},
		Description: "Basic user access",
	})
	if err != nil {
		return fmt.Errorf("set up auth: %w", err)
	}

	err = seedOwners(ctx, authService, cfg.shared.Owners, log)
	if err != nil {
		return err
	}

	latexCommand, err := latex.New(cfg.latex)
	if err != nil {
		return fmt.Errorf("set up latex: %w", err)
	}

	defer func() {
		err = errors.Join(err, latexCommand.Close())
	}()

	limiter, err := ratelimit.NewLimiter(cfg.shared.RateLimit.Requests, cfg.shared.RateLimit.Period, cfg.shared.RateLimit.Cooldown)
	if err != nil {
		return fmt.Errorf("set up rate limit: %w", err)
	}

	client, err := open(ctx, database, log)
	if err != nil {
		return fmt.Errorf("open whatsapp: %w", err)
	}

	router := command.NewRouter("!", authService, latexCommand)

	runtime := bot.New(client, router, log, bot.Options{
		Groups:      true,
		OwnMessages: cfg.shared.OwnMessages,
		Limiter:     limiter,
		MaxInFlight: cfg.shared.MaxInFlight,
	})

	return runtime.Run(ctx) //nolint:wrapcheck // exitStatus reads the runtime's own errors.
}

func seedOwners(ctx context.Context, service *auth.Service, owners []string, log *slog.Logger) error {
	seeded, err := service.SeedOwners(ctx, owners)
	if err != nil {
		return fmt.Errorf("seed owners: %w", err)
	}

	for _, jid := range seeded.Created {
		log.InfoContext(ctx, "seeded owner", "jid", jid)
	}

	for _, jid := range seeded.Skipped {
		log.WarnContext(ctx, "owner already registered with another rank, left unchanged", "jid", jid)
	}

	for _, jid := range seeded.Inactive {
		log.WarnContext(ctx, "owner belongs to a deactivated user, left unchanged", "jid", jid)
	}

	return nil
}

// exitStatus is the process exit status for the error run returned: 78 when the
// WhatsApp session ended, 0 when ctx was cancelled to stop the bot, 1
// otherwise.
func exitStatus(ctx context.Context, err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, new(*bot.SessionEndedError)):
		return exitConfig
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return exitOK
	default:
		return exitFailure
	}
}

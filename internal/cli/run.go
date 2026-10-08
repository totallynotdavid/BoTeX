package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/ratelimit"
	"github.com/totallynotdavid/botkit/internal/sqlite"
)

// Run is the run subcommand with the environment, the logger and the WhatsApp
// connection supplied. It returns what the bot returned, for ExitStatus.
func Run(ctx context.Context, cmd Command, env *config.Env, log *slog.Logger, open OpenClient) error {
	cfg, err := readSettings(cmd, env)
	if err != nil {
		return err
	}

	return run(ctx, cfg, log, open)
}

// run builds the bot from cfg and runs it until ctx is done or the WhatsApp
// session ends.
func run(ctx context.Context, cfg settings, log *slog.Logger, open OpenClient) (err error) {
	setup, err := prepare(ctx, cfg, log)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, setup.close()) }()

	client, err := open(ctx, setup.database, log)
	if err != nil {
		return fmt.Errorf("open whatsapp: %w", err)
	}

	return runBot(ctx, client, setup.built, setup.limiter, cfg, log)
}

type prepared struct {
	database *sql.DB
	users    *auth.Service
	built    Built
	limiter  *ratelimit.Limiter
}

func (p prepared) close() error {
	var err error
	if p.built.Close != nil {
		err = errors.Join(err, p.built.Close())
	}

	return errors.Join(err, p.database.Close())
}

func prepare(ctx context.Context, cfg settings, log *slog.Logger) (prepared, error) {
	database, err := sqlite.Open(ctx, cfg.shared.Store)
	if err != nil {
		return prepared{}, fmt.Errorf("open store: %w", err)
	}

	users, err := auth.New(ctx, database, cfg.ranks...)
	if err != nil {
		return prepared{}, errors.Join(fmt.Errorf("set up auth: %w", err), database.Close())
	}

	err = seedOwners(ctx, users, cfg.shared.Owners, log)
	if err != nil {
		return prepared{}, errors.Join(err, database.Close())
	}

	built, err := cfg.build(ctx, database, users, log)
	if err != nil {
		return prepared{}, errors.Join(err, database.Close())
	}

	limiter, err := ratelimit.NewLimiter(cfg.shared.RateLimit.Requests, cfg.shared.RateLimit.Period, cfg.shared.RateLimit.Cooldown)
	if err != nil {
		return prepared{}, errors.Join(fmt.Errorf("set up rate limit: %w", err), closeBuilt(built), database.Close())
	}

	return prepared{database: database, users: users, built: built, limiter: limiter}, nil
}

func closeBuilt(built Built) error {
	if built.Close == nil {
		return nil
	}

	return built.Close()
}

func runBot(ctx context.Context, client bot.Transport, built Built, limiter *ratelimit.Limiter, cfg settings, log *slog.Logger) error {
	allowOnly := make([]bot.JID, len(cfg.shared.AllowOnly))
	for i, jid := range cfg.shared.AllowOnly {
		allowOnly[i] = bot.JID(jid)
	}

	runtime := bot.New(client, built.App, log, bot.Options{
		Groups:      built.Groups,
		OwnMessages: cfg.shared.OwnMessages,
		AllowOnly:   allowOnly,
		Limiter:     limiter,
		MaxInFlight: cfg.shared.MaxInFlight,
	})

	return runtime.Run(ctx) //nolint:wrapcheck // ExitStatus reads the runtime's own errors.
}

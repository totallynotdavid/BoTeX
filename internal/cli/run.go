package cli

import (
	"context"
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
	database, err := sqlite.Open(ctx, cfg.shared.Store)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	defer func() {
		err = errors.Join(err, database.Close())
	}()

	users, err := auth.New(ctx, database, cfg.ranks...)
	if err != nil {
		return fmt.Errorf("set up auth: %w", err)
	}

	err = seedOwners(ctx, users, cfg.shared.Owners, log)
	if err != nil {
		return err
	}

	built, err := cfg.build(ctx, database, users, log)
	if err != nil {
		return err
	}

	defer func() {
		if built.Close != nil {
			err = errors.Join(err, built.Close())
		}
	}()

	limiter, err := ratelimit.NewLimiter(cfg.shared.RateLimit.Requests, cfg.shared.RateLimit.Period, cfg.shared.RateLimit.Cooldown)
	if err != nil {
		return fmt.Errorf("set up rate limit: %w", err)
	}

	client, err := open(ctx, database, log)
	if err != nil {
		return fmt.Errorf("open whatsapp: %w", err)
	}

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

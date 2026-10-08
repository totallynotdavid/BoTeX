package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

type followUpRunner interface {
	FollowUp(ctx context.Context, transport bot.Transport, now time.Time) (int, error)
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

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	followUpDone := make(chan struct{})
	if app, ok := built.App.(followUpRunner); ok {
		go runFollowUps(runCtx, client, app, log, followUpDone)
	} else {
		close(followUpDone)
	}

	err := runtime.Run(runCtx)

	cancel()
	<-followUpDone

	return err //nolint:wrapcheck // ExitStatus reads the runtime's own errors.
}

// runFollowUps wakes hourly. The flow app applies the stricter 24-hour claim
// window and checks explicit consent in SQLite. Keeping the scheduler here
// means the flow app remains usable by the offline REPL and fake tests without
// making every app responsible for process lifetime.
func runFollowUps(ctx context.Context, client bot.Transport, app followUpRunner, log *slog.Logger, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_, err := app.FollowUp(ctx, client, time.Now())
			if err != nil {
				log.ErrorContext(ctx, "proactive flow follow-up failed", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

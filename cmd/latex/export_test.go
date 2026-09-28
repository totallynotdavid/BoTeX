package main

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/config"
)

type Settings = settings

const (
	ExitOK      = exitOK
	ExitFailure = exitFailure
	ExitConfig  = exitConfig
	ExitUsage   = exitUsage
)

func ReadSettings(env *config.Env) (Settings, error) { return readSettings(env) }

func ExitStatus(ctx context.Context, err error) int { return exitStatus(ctx, err) }

func Execute(ctx context.Context, args []string) int { return execute(ctx, args) }

// Run is run with the WhatsApp connection open supplies.
func Run(ctx context.Context, cfg Settings, log *slog.Logger, open func(context.Context, *sql.DB, *slog.Logger) (bot.Client, error)) error {
	return run(ctx, cfg, log, open)
}

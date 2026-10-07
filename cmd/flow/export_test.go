package main

import (
	"context"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
)

// Run is the run subcommand with the environment and the WhatsApp connection
// the caller supplies.
func Run(ctx context.Context, env *config.Env, log *slog.Logger, open cli.OpenClient) error {
	return cli.Run(ctx, botCommand(), env, log, open) //nolint:wrapcheck // the tests read Run's error as it is.
}

// Command is the bot's description as the binary runs it.
func Command() cli.Command { return botCommand() }

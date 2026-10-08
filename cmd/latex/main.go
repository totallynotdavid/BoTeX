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
	"fmt"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/command"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/latex"
)

const userRankLevel = 100

func main() {
	cli.Main(botCommand())
}

func botCommand() cli.Command {
	return cli.Command{
		Name: "latex",
		Ranks: []auth.Rank{{
			Name:        "user",
			Level:       userRankLevel,
			Commands:    []string{"help", "latex"},
			Description: "Basic user access",
		}},
		Groups:    true,
		Configure: configure,
	}
}

func configure(env *config.Env) cli.Build {
	cfg := latex.ConfigFromEnv(env)

	return func(ctx context.Context, database *sql.DB, users *auth.Service, _ *slog.Logger) (cli.Built, error) {
		renderer, err := latex.New(cfg)
		if err != nil {
			return cli.Built{}, fmt.Errorf("set up latex: %w", err)
		}

		app, err := latex.NewApp(ctx, command.NewRouter("!", users, renderer), users, database)
		if err != nil {
			return cli.Built{}, fmt.Errorf("set up latex conversation: %w", errors.Join(err, renderer.Close()))
		}

		return cli.Built{
			App:   app,
			Close: renderer.Close,
		}, nil
	}
}

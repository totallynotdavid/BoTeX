// Command flow is the WhatsApp bot that walks each user through a
// conversation flow.
//
//	flow [run]           run the bot
//	flow pair [--phone +<digits>]
//	                     link the bot to a WhatsApp account
//	flow handoff list | clear <jid>
//	                     show the users waiting for a person, or record that
//	                     a person took one over
//
// Its exit statuses are those of package cli. It answers direct messages only.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

func main() {
	cli.Main(botCommand())
}

func botCommand() cli.Command {
	return cli.Command{
		Name:        "flow",
		RateLimit:   flow.DefaultRateLimit(),
		Subcommands: []cli.Subcommand{handoffSubcommand()},
		Configure:   configure,
	}
}

func configure(env *config.Env) cli.Build {
	cfg := flow.ConfigFromEnv(env)

	return func(ctx context.Context, database *sql.DB, _ *auth.Service, log *slog.Logger) (cli.Built, error) {
		actions := flow.NewActions(cfg.VoucherDir)

		definition, err := load(cfg.File, actions)
		if err != nil {
			return cli.Built{}, err
		}

		store, err := flow.NewStore(ctx, database)
		if err != nil {
			return cli.Built{}, fmt.Errorf("set up flow store: %w", err)
		}

		return cli.Built{App: flow.New(definition, store, actions, cfg, log)}, nil
	}
}

// load reads the flow file at path, or returns the selected built-in flow when
// path is empty. A path that cannot be loaded is an error, so a typo in
// FLOW_FILE never serves a built-in flow to real users.
func load(path string, actions *flow.Actions) (*fsm.Flow, error) {
	if path == "" {
		definition, err := fsm.EngagingExample(actions.Knows)
		if err != nil {
			return nil, fmt.Errorf("load built-in flow: %w", err)
		}

		return definition, nil
	}

	definition, err := fsm.Load(path, actions.Knows)
	if err != nil {
		return nil, fmt.Errorf("load %s %q: %w", flow.KeyFile, path, err)
	}

	return definition, nil
}

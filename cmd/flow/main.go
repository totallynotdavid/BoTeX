// Command flow is the WhatsApp bot that walks each user through a
// conversation flow.
//
//	flow [run]           run the bot
//	flow pair [--phone +<digits>]
//	                     link the bot to a WhatsApp account
//
// Its exit statuses are those of package cli. It answers direct messages only.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

func main() {
	cli.Main(botCommand())
}

// A customer walking a menu sends several messages a minute.
const requestsPerMinute = 20

func botCommand() cli.Command {
	return cli.Command{
		Name:      "flow",
		RateLimit: config.RateLimit{Requests: requestsPerMinute, Period: time.Minute, Cooldown: time.Minute},
		Configure: configure,
	}
}

func configure(env *config.Env) cli.Build {
	cfg := flow.ConfigFromEnv(env)

	return func(ctx context.Context, database *sql.DB, _ *auth.Service, log *slog.Logger) (cli.Built, error) {
		definition, err := load(cfg.File)
		if err != nil {
			return cli.Built{}, err
		}

		store, err := flow.NewStore(ctx, database)
		if err != nil {
			return cli.Built{}, fmt.Errorf("set up flow store: %w", err)
		}

		app, err := flow.New(definition, store, flow.NewActions(cfg.VoucherDir), cfg, log)
		if err != nil {
			return cli.Built{}, fmt.Errorf("set up flow: %w", err)
		}

		return cli.Built{App: app}, nil
	}
}

// load reads the flow file at path, or returns the built-in example when path
// is empty. A path that cannot be loaded is an error, so a typo in FLOW_FILE
// never serves the example to real users.
func load(path string) (*fsm.Flow, error) {
	if path == "" {
		definition, err := fsm.Example()
		if err != nil {
			return nil, fmt.Errorf("load example flow: %w", err)
		}

		return definition, nil
	}

	definition, err := fsm.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load %s %q: %w", flow.KeyFile, path, err)
	}

	return definition, nil
}

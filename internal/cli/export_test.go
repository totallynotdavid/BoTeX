package cli

import (
	"context"

	"github.com/totallynotdavid/botkit/internal/config"
)

func Execute(ctx context.Context, cmd Command, args []string) int { return execute(ctx, cmd, args) }

// SharedSettings is the shared configuration cmd reads from env.
func SharedSettings(cmd Command, env *config.Env) (config.Shared, error) {
	cfg, err := readSettings(cmd, env)

	return cfg.shared, err
}

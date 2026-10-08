package cli

import (
	"github.com/totallynotdavid/botkit/internal/config"
)

// SharedSettings is the shared configuration cmd reads from env.
func SharedSettings(cmd Command, env *config.Env) (config.Shared, error) {
	cfg, err := readSettings(cmd, env)

	return cfg.shared, err
}

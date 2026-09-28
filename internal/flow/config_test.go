package flow_test

import (
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/flow"
)

func envOf(vars map[string]string) *config.Env {
	return config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	})
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Parallel()

	env := envOf(nil)

	got := flow.ConfigFromEnv(env)
	want := flow.Config{VoucherDir: "vouchers"}

	if got != want {
		t.Errorf("ConfigFromEnv() = %+v, want %+v", got, want)
	}

	err := env.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	t.Parallel()

	env := envOf(map[string]string{
		flow.KeyFile:        "bot.json",
		flow.KeyVoucherDir:  "/srv/receipts",
		flow.KeyTypingDelay: "1500ms",
	})

	got := flow.ConfigFromEnv(env)
	want := flow.Config{File: "bot.json", VoucherDir: "/srv/receipts", TypingDelay: 1500 * time.Millisecond}

	if got != want {
		t.Errorf("ConfigFromEnv() = %+v, want %+v", got, want)
	}

	err := env.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestConfigFromEnvBadDelayNamesItsKey(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{"not a duration": "slow", "negative": "-1s"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := envOf(map[string]string{flow.KeyTypingDelay: value})

			flow.ConfigFromEnv(env)

			err := env.Err()
			if err == nil || !strings.Contains(err.Error(), flow.KeyTypingDelay) {
				t.Errorf("Err() = %v, want an error naming %s", err, flow.KeyTypingDelay)
			}
		})
	}
}

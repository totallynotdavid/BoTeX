package cli_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

// withLimit is a command that has its own rate-limit defaults.
func withLimit(limit config.RateLimit) cli.Command {
	cmd := idle()
	cmd.RateLimit = limit

	return cmd
}

func TestRateLimitDefaultsComeFromTheCommandAndTheKeysOverrideThem(t *testing.T) {
	t.Parallel()

	own := config.RateLimit{Requests: 20, Period: 2 * time.Minute, Cooldown: 30 * time.Second}

	tests := map[string]struct {
		cmd  cli.Command
		vars map[string]string
		want config.RateLimit
	}{
		"a command that sets nothing gets the shared defaults": {
			cmd:  idle(),
			vars: map[string]string{},
			want: config.DefaultShared().RateLimit,
		},
		"a command with its own defaults gets them": {
			cmd:  withLimit(own),
			vars: map[string]string{},
			want: own,
		},
		"the keys override the shared defaults": {
			cmd: idle(),
			vars: map[string]string{
				config.KeyRateLimitRequests: "7",
				config.KeyRateLimitPeriod:   "3m",
				config.KeyRateLimitCooldown: "9m",
			},
			want: config.RateLimit{Requests: 7, Period: 3 * time.Minute, Cooldown: 9 * time.Minute},
		},
		"the keys override the command's defaults, one at a time": {
			cmd: withLimit(own),
			vars: map[string]string{
				config.KeyRateLimitRequests: "7",
				config.KeyRateLimitCooldown: "9m",
			},
			want: config.RateLimit{Requests: 7, Period: own.Period, Cooldown: 9 * time.Minute},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := cli.SharedSettings(test.cmd, env(t, test.vars))
			if err != nil {
				t.Fatal(err)
			}

			if got.RateLimit != test.want {
				t.Errorf("rate limit = %+v, want %+v", got.RateLimit, test.want)
			}
		})
	}
}

func TestRunEnforcesTheCommandsRateLimit(t *testing.T) {
	t.Parallel()

	const requests = 2

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	app := newSpy()
	cmd := spyCommand(app, false, new(atomic.Bool))
	cmd.RateLimit = config.RateLimit{Requests: requests, Period: time.Hour, Cooldown: time.Hour}

	client := fake.New()
	done := startedOn(ctx, t, cmd, client, map[string]string{})

	for range requests + 1 {
		client.Deliver(bot.Message{Sender: alice})
	}

	for range requests {
		handledBy(t, app)
	}

	waitFor(t, func() bool { return len(client.Reactions()) == 1 })

	stop()

	err := stopped(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}

	if extra := len(app.handled); extra != 0 {
		t.Errorf("%d messages over the limit were handled", extra)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 10s")
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func TestDescribeReportsTheCommandsDefaults(t *testing.T) {
	t.Parallel()

	own := config.RateLimit{Requests: 20, Period: 2 * time.Minute, Cooldown: 30 * time.Second}

	defaults := map[string]string{}
	for _, entry := range cli.Describe(withLimit(own)) {
		defaults[entry.Key] = entry.Default
	}

	want := map[string]string{
		config.KeyRateLimitRequests: "20",
		config.KeyRateLimitPeriod:   "2m",
		config.KeyRateLimitCooldown: "30s",
	}

	for key, value := range want {
		if defaults[key] != value {
			t.Errorf("default of %s = %q, want %q", key, defaults[key], value)
		}
	}

	if _, ok := defaults[config.KeyStore]; !ok {
		t.Errorf("Describe() leaves out the shared setting %s", config.KeyStore)
	}
}

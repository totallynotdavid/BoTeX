package cli_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

var errRunBuild = errors.New("cannot build the app")

const runTestName = "run-test"

type runApp func(context.Context, bot.Message, *bot.Chat) error

func (f runApp) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	return f(ctx, msg, chat)
}

func runEnvironment(t *testing.T, vars map[string]string) *config.Env {
	t.Helper()

	if vars == nil {
		vars = make(map[string]string)
	}

	if _, set := vars[config.KeyStore]; !set {
		vars[config.KeyStore] = filepath.Join(t.TempDir(), "run.db")
	}

	return config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	})
}

func runCommand(app bot.App, closeApp func() error) cli.Command {
	return cli.Command{
		Name: runTestName,
		Configure: func(*config.Env) cli.Build {
			return func(context.Context, *sql.DB, *auth.Service, *slog.Logger) (cli.Built, error) {
				return cli.Built{App: app, Close: closeApp}, nil
			}
		},
	}
}

func TestRunClosesBuiltAppBeforeDatabase(t *testing.T) {
	t.Parallel()

	var database *sql.DB

	var (
		closeCalled         atomic.Bool
		closeQuerySucceeded atomic.Bool
	)

	closeApp := func() error {
		closeCalled.Store(true)

		_, err := database.ExecContext(context.Background(), "SELECT 1")
		if err != nil {
			return fmt.Errorf("query while app closes: %w", err)
		}

		closeQuerySucceeded.Store(true)

		return nil
	}

	app := runApp(func(context.Context, bot.Message, *bot.Chat) error { return nil })
	cmd := cli.Command{
		Name: runTestName,
		Configure: func(*config.Env) cli.Build {
			return func(_ context.Context, db *sql.DB, _ *auth.Service, _ *slog.Logger) (cli.Built, error) {
				database = db

				return cli.Built{App: app, Close: closeApp}, nil
			}
		},
	}

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	client := fake.New()

	go func() {
		done <- cli.Run(ctx, cmd, runEnvironment(t, map[string]string{}), slog.New(slog.DiscardHandler), func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) {
			return client, nil
		})
	}()

	err := client.WaitConnected(ctx)
	if err != nil {
		stop()
		t.Fatal(err)
	}

	stop()

	err = <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}

	if !closeCalled.Load() {
		t.Fatal("built app was not closed")
	}

	if !closeQuerySucceeded.Load() {
		t.Fatal("built app closed after database")
	}

	_, err = database.ExecContext(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("database remained open after built app cleanup")
	}
}

func TestRunPrepareErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cmd  cli.Command
		vars map[string]string
		want error
	}{
		"store open": {
			cmd:  runCommand(runApp(func(context.Context, bot.Message, *bot.Chat) error { return nil }), nil),
			vars: map[string]string{config.KeyStore: filepath.Join(t.TempDir(), "missing", "run.db")},
		},
		"build": {
			cmd: cli.Command{
				Name: runTestName,
				Configure: func(*config.Env) cli.Build {
					return func(context.Context, *sql.DB, *auth.Service, *slog.Logger) (cli.Built, error) {
						return cli.Built{}, errRunBuild
					}
				},
			},
			want: errRunBuild,
		},
		"rate limit": {
			cmd: cli.Command{
				Name:      runTestName,
				RateLimit: config.RateLimit{Period: time.Second},
				Configure: func(*config.Env) cli.Build {
					return func(context.Context, *sql.DB, *auth.Service, *slog.Logger) (cli.Built, error) {
						return cli.Built{App: runApp(func(context.Context, bot.Message, *bot.Chat) error { return nil })}, nil
					}
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertPrepareError(t, name, test.cmd, test.vars, test.want)
		})
	}
}

func assertPrepareError(t *testing.T, name string, cmd cli.Command, vars map[string]string, want error) {
	t.Helper()

	err := runPrepareCase(t, cmd, vars)
	if err == nil {
		t.Fatal("Run() succeeded, want prepare error")
	}

	if want != nil && !errors.Is(err, want) {
		t.Errorf("Run() = %v, want %v", err, want)
	}

	if name == "rate limit" && !strings.Contains(err.Error(), "invalid rate limit") {
		t.Errorf("Run() = %v, want invalid rate limit", err)
	}

	if name == "store open" && !strings.Contains(err.Error(), "open store") {
		t.Errorf("Run() = %v, want open store context", err)
	}
}

func runPrepareCase(t *testing.T, cmd cli.Command, vars map[string]string) error {
	t.Helper()

	err := cli.Run(t.Context(), cmd, runEnvironment(t, vars), slog.New(slog.DiscardHandler), func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) {
		return fake.New(), nil
	})
	if err != nil {
		return fmt.Errorf("run prepare case: %w", err)
	}

	return nil
}

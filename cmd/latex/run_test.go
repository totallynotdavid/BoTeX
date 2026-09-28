package main_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	latexcmd "github.com/totallynotdavid/botkit/cmd/latex"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/latex"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const (
	owner bot.JID = "51900000001@s.whatsapp.net"
	group bot.JID = "120363000000000000@g.us"
)

var errOpen = errors.New("whatsapp is unreachable")

type opener = func(context.Context, *sql.DB, *slog.Logger) (bot.Client, error)

func settings(t *testing.T, vars map[string]string) latexcmd.Settings {
	t.Helper()

	if _, set := vars[config.KeyStore]; !set {
		vars[config.KeyStore] = filepath.Join(t.TempDir(), "bot.db")
	}

	cfg, err := latexcmd.ReadSettings(config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	}))
	if err != nil {
		t.Fatal(err)
	}

	return cfg
}

func using(client bot.Client) opener {
	return func(context.Context, *sql.DB, *slog.Logger) (bot.Client, error) { return client, nil }
}

// started runs the bot in the background and returns what Run returned once it
// stops.
func started(ctx context.Context, cfg latexcmd.Settings, open opener) <-chan error {
	done := make(chan error, 1)

	go func() { done <- latexcmd.Run(ctx, cfg, slog.New(slog.DiscardHandler), open) }()

	return done
}

// startedOn runs the bot on client and waits until it is connected.
func startedOn(ctx context.Context, t *testing.T, client *fake.Client, vars map[string]string) <-chan error {
	t.Helper()

	done := started(ctx, settings(t, vars), using(client))

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return done
}

func stopped(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")

		return nil
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

func TestExitStatus(t *testing.T) {
	t.Parallel()

	live := context.Background()

	signalled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := map[string]struct {
		ctx  context.Context //nolint:containedctx // the context is the input under test.
		err  error
		want int
	}{
		"clean stop":             {ctx: live, want: latexcmd.ExitOK},
		"signal stop":            {ctx: signalled, err: context.Canceled, want: latexcmd.ExitOK},
		"signal stop, wrapped":   {ctx: signalled, err: errors.Join(context.Canceled, errOpen), want: latexcmd.ExitOK},
		"cancel nobody asked":    {ctx: live, err: context.Canceled, want: latexcmd.ExitFailure},
		"logged out":             {ctx: live, err: &bot.SessionEndedError{Reason: bot.LoggedOut}, want: latexcmd.ExitConfig},
		"not paired":             {ctx: live, err: &bot.SessionEndedError{Reason: bot.NotPaired}, want: latexcmd.ExitConfig},
		"ended while signalled":  {ctx: signalled, err: &bot.SessionEndedError{Reason: bot.Replaced}, want: latexcmd.ExitConfig},
		"session end and others": {ctx: live, err: errors.Join(errOpen, &bot.SessionEndedError{Reason: bot.Banned}), want: latexcmd.ExitConfig},
		"other failure":          {ctx: live, err: errOpen, want: latexcmd.ExitFailure},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := latexcmd.ExitStatus(test.ctx, test.err)
			if got != test.want {
				t.Errorf("ExitStatus(%v) = %d, want %d", test.err, got, test.want)
			}
		})
	}
}

func TestRunUnpairedStoreExits78(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	err := stopped(t, started(ctx, settings(t, map[string]string{}), using(fake.NewUnpaired())))

	var ended *bot.SessionEndedError
	if !errors.As(err, &ended) || ended.Reason != bot.NotPaired {
		t.Fatalf("Run() error = %v, want a SessionEndedError with reason not_paired", err)
	}

	if got := latexcmd.ExitStatus(ctx, err); got != latexcmd.ExitConfig {
		t.Errorf("exit status = %d, want %d", got, latexcmd.ExitConfig)
	}
}

func TestRunLoggedOutWhileRunningExits78(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client := fake.New()
	done := startedOn(ctx, t, client, map[string]string{})

	client.EndSession(bot.LoggedOut, "401")

	err := stopped(t, done)
	if got := latexcmd.ExitStatus(ctx, err); got != latexcmd.ExitConfig {
		t.Errorf("Run() error = %v, exit status = %d, want %d", err, got, latexcmd.ExitConfig)
	}
}

func TestRunSignalExits0(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	done := startedOn(ctx, t, fake.New(), map[string]string{})

	stop()

	err := stopped(t, done)
	if got := latexcmd.ExitStatus(ctx, err); got != latexcmd.ExitOK {
		t.Errorf("Run() error = %v, exit status = %d, want %d", err, got, latexcmd.ExitOK)
	}
}

func TestRunWhatsappFailureExits1(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	open := func(context.Context, *sql.DB, *slog.Logger) (bot.Client, error) { return nil, errOpen }

	err := stopped(t, started(ctx, settings(t, map[string]string{}), open))
	if !errors.Is(err, errOpen) {
		t.Fatalf("Run() error = %v, want %v", err, errOpen)
	}

	if got := latexcmd.ExitStatus(ctx, err); got != latexcmd.ExitFailure {
		t.Errorf("exit status = %d, want %d", got, latexcmd.ExitFailure)
	}
}

func TestRunStoreFailureExits1(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	missing := filepath.Join(t.TempDir(), "missing", "bot.db")

	err := stopped(t, started(ctx, settings(t, map[string]string{config.KeyStore: missing}), using(fake.New())))
	if err == nil {
		t.Fatal("Run() succeeded on a store in a missing directory")
	}

	if got := latexcmd.ExitStatus(ctx, err); got != latexcmd.ExitFailure {
		t.Errorf("Run() error = %v, exit status = %d, want %d", err, got, latexcmd.ExitFailure)
	}
}

func TestRunAnswersSeededOwnerInDirectChatsAndGroups(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	client := fake.New()
	done := startedOn(ctx, t, client, map[string]string{config.KeyOwners: string(owner)})

	client.Deliver(bot.Message{Sender: owner, Text: "!help"})
	waitFor(t, func() bool { return len(client.Sent()) == 1 })

	// Groups reach the router, which refuses one nobody registered.
	client.Deliver(bot.Message{Sender: owner, Chat: group, Group: true, Text: "!help"})
	waitFor(t, func() bool { return len(client.Sent()) == 2 })

	sent := client.Sent()
	if sent[0].To != owner {
		t.Errorf("help went to %q, want the owner's chat", sent[0].To)
	}

	if sent[1].To != group {
		t.Errorf("group notice went to %q, want the group", sent[1].To)
	}

	stop()

	err := stopped(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestReadSettingsNamesEveryBadKey(t *testing.T) {
	t.Parallel()

	bad := map[string]string{config.KeyMaxInFlight: "many", latex.KeyTimeout: "soon"}

	_, err := latexcmd.ReadSettings(config.New(func(key string) (string, bool) {
		raw, ok := bad[key]

		return raw, ok
	}))
	if err == nil {
		t.Fatal("ReadSettings() succeeded on bad values")
	}

	for key := range bad {
		if !strings.Contains(err.Error(), key+"=") {
			t.Errorf("ReadSettings() error = %v, want it to name %s", err, key)
		}
	}
}

func TestExecuteRejectsBadUsage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"bogus"}, {"run", "extra"}, {"pair", "--nope"}} {
		got := latexcmd.Execute(t.Context(), args)
		if got != latexcmd.ExitUsage {
			t.Errorf("Execute(%q) = %d, want %d", args, got, latexcmd.ExitUsage)
		}
	}
}

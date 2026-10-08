package cli_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
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

const (
	alice bot.JID = "51900000001@s.whatsapp.net"
	bob   bot.JID = "51900000002@s.whatsapp.net"
	group bot.JID = "120363000000000000@g.us"
)

var (
	errOpen  = errors.New("whatsapp is unreachable")
	errBuild = errors.New("cannot build the app")
)

type opener = func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error)

// spy is a bot that reports the sender of every message it handles.
type spy struct {
	handled chan bot.JID
}

func newSpy() *spy { return &spy{handled: make(chan bot.JID, 16)} }

func (s *spy) Handle(_ context.Context, msg bot.Message, _ *bot.Chat) error {
	s.handled <- msg.User

	return nil
}

// spyCommand is a command whose bot is app. closed is set when the bot is
// closed.
func spyCommand(app bot.App, groups bool, closed *atomic.Bool) cli.Command {
	return cli.Command{
		Name: "spy",
		Configure: func(*config.Env) cli.Build {
			return func(context.Context, *sql.DB, *auth.Service, *slog.Logger) (cli.Built, error) {
				return cli.Built{
					App:    app,
					Groups: groups,
					Close: func() error {
						closed.Store(true)

						return nil
					},
				}, nil
			}
		},
	}
}

func env(t *testing.T, vars map[string]string) *config.Env {
	t.Helper()

	if _, set := vars[config.KeyStore]; !set {
		vars[config.KeyStore] = filepath.Join(t.TempDir(), "bot.db")
	}

	return config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	})
}

func using(client bot.Transport) opener {
	return func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) { return client, nil }
}

// started runs the bot in the background and returns what Run returned once it
// stops.
func started(ctx context.Context, cmd cli.Command, environment *config.Env, open opener) <-chan error {
	done := make(chan error, 1)

	go func() { done <- cli.Run(ctx, cmd, environment, slog.New(slog.DiscardHandler), open) }()

	return done
}

// startedOn runs the bot on client and waits until it is connected.
func startedOn(ctx context.Context, t *testing.T, cmd cli.Command, client *fake.Client, vars map[string]string) <-chan error {
	t.Helper()

	done := started(ctx, cmd, env(t, vars), using(client))

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

func handledBy(t *testing.T, app *spy) bot.JID {
	t.Helper()

	select {
	case user := <-app.handled:
		return user
	case <-time.After(10 * time.Second):
		t.Fatal("no message was handled within 10s")

		return ""
	}
}

func idle() cli.Command {
	return spyCommand(newSpy(), false, new(atomic.Bool))
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
		"clean stop":             {ctx: live, want: cli.ExitOK},
		"signal stop":            {ctx: signalled, err: context.Canceled, want: cli.ExitOK},
		"signal stop, wrapped":   {ctx: signalled, err: errors.Join(context.Canceled, errOpen), want: cli.ExitOK},
		"cancel nobody asked":    {ctx: live, err: context.Canceled, want: cli.ExitFailure},
		"logged out":             {ctx: live, err: &bot.SessionEndedError{Reason: bot.LoggedOut}, want: cli.ExitConfig},
		"not paired":             {ctx: live, err: &bot.SessionEndedError{Reason: bot.NotPaired}, want: cli.ExitConfig},
		"ended while signalled":  {ctx: signalled, err: &bot.SessionEndedError{Reason: bot.Replaced}, want: cli.ExitConfig},
		"session end and others": {ctx: live, err: errors.Join(errOpen, &bot.SessionEndedError{Reason: bot.Banned}), want: cli.ExitConfig},
		"other failure":          {ctx: live, err: errOpen, want: cli.ExitFailure},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := cli.ExitStatus(test.ctx, test.err)
			if got != test.want {
				t.Errorf("ExitStatus(%v) = %d, want %d", test.err, got, test.want)
			}
		})
	}
}

func TestRunUnpairedStoreExits78(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	err := stopped(t, started(ctx, idle(), env(t, map[string]string{}), using(fake.NewUnpaired())))

	var ended *bot.SessionEndedError
	if !errors.As(err, &ended) || ended.Reason != bot.NotPaired {
		t.Fatalf("Run() error = %v, want a SessionEndedError with reason not_paired", err)
	}

	if got := cli.ExitStatus(ctx, err); got != cli.ExitConfig {
		t.Errorf("exit status = %d, want %d", got, cli.ExitConfig)
	}
}

func TestRunLoggedOutWhileRunningExits78(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client := fake.New()
	done := startedOn(ctx, t, idle(), client, map[string]string{})

	client.EndSession(bot.LoggedOut, "401")

	err := stopped(t, done)
	if got := cli.ExitStatus(ctx, err); got != cli.ExitConfig {
		t.Errorf("Run() error = %v, exit status = %d, want %d", err, got, cli.ExitConfig)
	}
}

func TestRunSignalExits0AndClosesTheBot(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	var closed atomic.Bool

	done := startedOn(ctx, t, spyCommand(newSpy(), false, &closed), fake.New(), map[string]string{})

	stop()

	err := stopped(t, done)
	if got := cli.ExitStatus(ctx, err); got != cli.ExitOK {
		t.Errorf("Run() error = %v, exit status = %d, want %d", err, got, cli.ExitOK)
	}

	if !closed.Load() {
		t.Error("the bot was not closed when Run returned")
	}
}

func TestRunWhatsappFailureExits1AndClosesTheBot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	open := func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) { return nil, errOpen }

	var closed atomic.Bool

	err := stopped(t, started(ctx, spyCommand(newSpy(), false, &closed), env(t, map[string]string{}), open))
	if !errors.Is(err, errOpen) {
		t.Fatalf("Run() error = %v, want %v", err, errOpen)
	}

	if got := cli.ExitStatus(ctx, err); got != cli.ExitFailure {
		t.Errorf("exit status = %d, want %d", got, cli.ExitFailure)
	}

	if !closed.Load() {
		t.Error("the bot was not closed after the connection failed")
	}
}

func TestRunStoreFailureExits1(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	missing := filepath.Join(t.TempDir(), "missing", "bot.db")

	err := stopped(t, started(ctx, idle(), env(t, map[string]string{config.KeyStore: missing}), using(fake.New())))
	if err == nil {
		t.Fatal("Run() succeeded on a store in a missing directory")
	}

	if got := cli.ExitStatus(ctx, err); got != cli.ExitFailure {
		t.Errorf("Run() error = %v, exit status = %d, want %d", err, got, cli.ExitFailure)
	}
}

func TestRunBuildFailureExits1(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cmd := cli.Command{
		Name: "spy",
		Configure: func(*config.Env) cli.Build {
			return func(context.Context, *sql.DB, *auth.Service, *slog.Logger) (cli.Built, error) {
				return cli.Built{}, errBuild
			}
		},
	}

	err := stopped(t, started(ctx, cmd, env(t, map[string]string{}), using(fake.New())))
	if !errors.Is(err, errBuild) {
		t.Fatalf("Run() error = %v, want %v", err, errBuild)
	}

	if got := cli.ExitStatus(ctx, err); got != cli.ExitFailure {
		t.Errorf("exit status = %d, want %d", got, cli.ExitFailure)
	}
}

func TestRunBadSettingsFailBeforeAnythingOpens(t *testing.T) {
	t.Parallel()

	vars := map[string]string{config.KeyMaxInFlight: "many", config.KeyAllowOnly: "not-a-jid"}

	var opened atomic.Bool

	open := func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) {
		opened.Store(true)

		return fake.New(), nil
	}

	err := stopped(t, started(t.Context(), idle(), env(t, maps.Clone(vars)), open))
	if err == nil {
		t.Fatal("Run() succeeded on bad settings")
	}

	for key := range vars {
		if !strings.Contains(err.Error(), key+"=") {
			t.Errorf("Run() error = %v, want it to name %s", err, key)
		}
	}

	if opened.Load() {
		t.Error("the WhatsApp connection was opened with bad settings")
	}
}

// handledFrom runs a bot that takes groups or not, with allow as its
// BOTKIT_ALLOW_ONLY, delivers send, and returns the senders of the want
// messages its app handled, sorted. It fails when the app handles more.
//
// A refused message is refused as it arrives and stopping waits for running
// handlers, so a message that should not be handled has been by then.
func handledFrom(t *testing.T, groups bool, allow string, send []bot.Message, want int) []bot.JID {
	t.Helper()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	app := newSpy()
	client := fake.New()
	done := startedOn(ctx, t, spyCommand(app, groups, new(atomic.Bool)), client,
		map[string]string{config.KeyAllowOnly: allow})

	for i := range send {
		client.Deliver(send[i])
	}

	got := make([]bot.JID, 0, want)
	for range want {
		got = append(got, handledBy(t, app))
	}

	stop()

	err := stopped(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}

	if extra := len(app.handled); extra != 0 {
		t.Errorf("%d more messages were handled than expected", extra)
	}

	slices.Sort(got)

	return got
}

func TestRunAnswersOnlyWhoIsAllowedAndWhereTheBotSays(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		groups bool
		allow  string
		send   []bot.Message
		want   []bot.JID
	}{
		"everyone when the list is empty": {
			send: []bot.Message{{Sender: alice}, {Sender: bob}},
			want: []bot.JID{alice, bob},
		},
		"only who is listed": {
			allow: string(bob),
			send:  []bot.Message{{Sender: alice}, {Sender: bob}},
			want:  []bot.JID{bob},
		},
		"groups when the bot takes them": {
			groups: true,
			send:   []bot.Message{{Sender: alice, Chat: group, Group: true}, {Sender: bob}},
			want:   []bot.JID{alice, bob},
		},
		"no groups when the bot does not": {
			send: []bot.Message{{Sender: alice, Chat: group, Group: true}, {Sender: bob}},
			want: []bot.JID{bob},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := handledFrom(t, test.groups, test.allow, test.send, len(test.want))
			if !slices.Equal(got, test.want) {
				t.Errorf("handled messages from %v, want %v", got, test.want)
			}
		})
	}
}

func TestExecuteRejectsBadUsage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"bogus"}, {"run", "extra"}, {"pair", "--nope"}} {
		got := cli.Execute(t.Context(), idle(), args)
		if got != cli.ExitUsage {
			t.Errorf("Execute(%q) = %d, want %d", args, got, cli.ExitUsage)
		}
	}
}

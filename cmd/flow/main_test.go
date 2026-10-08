package main_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	flowcmd "github.com/totallynotdavid/botkit/cmd/flow"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const (
	alice bot.JID = "51900000001@s.whatsapp.net"
	bob   bot.JID = "51900000002@s.whatsapp.net"
	group bot.JID = "120363000000000000@g.us"

	// greeting is the stable opening shared by the built-in flow.
	greeting = "¡Bienvenidx! Es un placer ayudarte a empezar."

	typoFlow = `{"start_node":"A","nodes":{` +
		`"A":{"message":{"type":"text","content":"hi"},"action":"launch_rocket"},` +
		`"NEEDS_ASSISTANCE":{"message":{"type":"text","content":"a person will help"}}}}`
)

func environment(t *testing.T, vars map[string]string) *config.Env {
	t.Helper()

	vars[config.KeyStore] = filepath.Join(t.TempDir(), "bot.db")
	vars[flow.KeyVoucherDir] = t.TempDir()

	return config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	})
}

func using(client bot.Transport) cli.OpenClient {
	return func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) { return client, nil }
}

// run starts the bot and returns what Run returned once it stops.
func run(ctx context.Context, env *config.Env, client bot.Transport) <-chan error {
	done := make(chan error, 1)

	go func() { done <- flowcmd.Run(ctx, env, slog.New(slog.DiscardHandler), using(client)) }()

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

// converse runs the bot with vars, delivers send, stops the bot once want
// replies are out and returns every reply sent. Stopping waits for the handlers
// still running, so a reply that should not exist has been sent by then.
func converse(t *testing.T, vars map[string]string, send []bot.Message, want int) []fake.Sent {
	t.Helper()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	client := fake.New()
	done := run(ctx, environment(t, vars), client)

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for i := range send {
		client.Deliver(send[i])
	}

	waitFor(t, func() bool { return len(client.Sent()) >= want })

	stop()

	err = stopped(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}

	return client.Sent()
}

func TestRunAnswersDirectMessagesFromAllowedSenders(t *testing.T) {
	t.Parallel()

	const hello = "hola"

	tests := map[string]struct {
		vars map[string]string
		send []bot.Message
		want []bot.JID
	}{
		"a new user gets the greeting": {
			send: []bot.Message{{Sender: alice, Text: hello}},
			want: []bot.JID{alice},
		},
		"a group message gets no reply": {
			send: []bot.Message{{Sender: alice, Chat: group, Group: true, Text: hello}, {Sender: bob, Text: hello}},
			want: []bot.JID{bob},
		},
		"a sender outside BOTKIT_ALLOW_ONLY gets no reply": {
			vars: map[string]string{config.KeyAllowOnly: string(bob)},
			send: []bot.Message{{Sender: alice, Text: hello}, {Sender: bob, Text: hello}},
			want: []bot.JID{bob},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{}
			maps.Copy(vars, test.vars)

			got := make([]bot.JID, 0, len(test.want))

			for _, reply := range converse(t, vars, test.send, len(test.want)) {
				got = append(got, reply.To)

				if !strings.Contains(reply.Text, greeting) {
					t.Errorf("reply to %q = %q, want it to greet with %q", reply.To, reply.Text, greeting)
				}
			}

			if !slices.Equal(got, test.want) {
				t.Errorf("replies went to %v, want %v", got, test.want)
			}
		})
	}
}

func TestRunStartupFailuresExitWithTheirStatus(t *testing.T) {
	t.Parallel()

	badFlow := filepath.Join(t.TempDir(), "flow.json")

	err := os.WriteFile(badFlow, []byte(typoFlow), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	missingFlow := filepath.Join(t.TempDir(), "typo.json")

	tests := map[string]struct {
		vars   map[string]string
		client *fake.Client
		want   int
		names  []string
	}{
		"a flow file with an unknown action": {
			vars:   map[string]string{flow.KeyFile: badFlow},
			client: fake.New(),
			want:   cli.ExitFailure,
			names:  []string{"launch_rocket"},
		},
		"a flow file that does not exist": {
			vars:   map[string]string{flow.KeyFile: missingFlow},
			client: fake.New(),
			want:   cli.ExitFailure,
			names:  []string{flow.KeyFile, missingFlow},
		},
		"an unpaired store": {
			vars:   map[string]string{},
			client: fake.NewUnpaired(),
			want:   cli.ExitConfig,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			err := stopped(t, run(ctx, environment(t, test.vars), test.client))
			if err == nil {
				t.Fatal("Run() succeeded")
			}

			got := cli.ExitStatus(ctx, err)
			if got != test.want {
				t.Errorf("Run() error = %v, exit status = %d, want %d", err, got, test.want)
			}

			for _, name := range test.names {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("Run() error = %q, want it to name %q", err, name)
				}
			}
		})
	}
}

// answerOneAtATime delivers count messages, each after the reply to the one
// before, so the concurrency cap never refuses one.
func answerOneAtATime(t *testing.T, client *fake.Client, count int) {
	t.Helper()

	for i := range count {
		client.Deliver(bot.Message{Sender: alice, Text: "hola"})
		waitFor(t, func() bool { return len(client.Sent()) == i+1 })
	}
}

func TestRunAllowsTwentyRequestsAMinuteAndTheKeyOverridesIt(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		vars    map[string]string
		allowed int
	}{
		"the flow default":           {allowed: 20},
		"BOTKIT_RATE_LIMIT_REQUESTS": {vars: map[string]string{config.KeyRateLimitRequests: "3"}, allowed: 3},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, stop := context.WithCancel(t.Context())
			defer stop()

			vars := map[string]string{}
			maps.Copy(vars, test.vars)

			client := fake.New()
			done := run(ctx, environment(t, vars), client)

			err := client.WaitConnected(ctx)
			if err != nil {
				t.Fatal(err)
			}

			answerOneAtATime(t, client, test.allowed)

			limited := client.Deliver(bot.Message{Sender: alice, Text: "hola"})

			waitFor(t, func() bool { return len(client.Sent()) == test.allowed+1 })
			stop()

			err = stopped(t, done)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Run() error = %v, want context.Canceled", err)
			}

			reactions := client.Reactions()
			if len(reactions) != 1 || reactions[0].MessageID != limited.ID {
				t.Errorf("reactions = %+v, want one on message %s, the one past the limit", reactions, limited.ID)
			}

			// One reply per allowed message, then the notice.
			if got := len(client.Sent()); got != test.allowed+1 {
				t.Errorf("%d messages sent, want %d replies and the rate-limit notice", got, test.allowed)
			}
		})
	}
}

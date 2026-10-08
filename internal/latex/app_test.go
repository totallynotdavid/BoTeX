package latex_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/command"
	"github.com/totallynotdavid/botkit/internal/latex"
	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

type welcomeRouter func(context.Context, bot.Message, *bot.Chat) error

func (f welcomeRouter) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	return f(ctx, msg, chat)
}

type permissions func(context.Context, string, string, string) (auth.Decision, error)

const lidAlice = "lid:alice"

func (p permissions) Authorize(ctx context.Context, user, group, name string) (auth.Decision, error) {
	return p(ctx, user, group, name)
}

func newDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "latex.db"), sqlite.WithoutSync())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		err := database.Close()
		if err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	return database
}

func startApp(t *testing.T, database *sql.DB, perms command.Permissions) (*fake.Client, context.CancelFunc, <-chan error) {
	t.Helper()

	client := fake.New()

	router := welcomeRouter(func(ctx context.Context, _ bot.Message, chat *bot.Chat) error {
		return chat.Send(ctx, "command handled")
	})

	app, err := latex.NewApp(t.Context(), router, perms, database)
	if err != nil {
		t.Fatal(err)
	}

	runtime := bot.New(client, app, slog.New(slog.DiscardHandler), bot.Options{})

	ctx, stop := context.WithCancel(t.Context())
	t.Cleanup(stop)

	done := make(chan error, 1)

	go func() { done <- runtime.Run(ctx) }()

	err = client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return client, stop, done
}

func waitForTurn(t *testing.T, client *fake.Client, sent, reactions int) {
	t.Helper()

	err := client.WaitForWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	gotSent := len(client.Sent())
	gotReactions := len(client.Reactions())

	if gotSent < sent || gotReactions < reactions {
		t.Fatalf("turn completed with sent=%d reactions=%d, want at least sent=%d reactions=%d", gotSent, gotReactions, sent, reactions)
	}
}

func TestAppGuidesAUserThroughAnOfflineConversation(t *testing.T) {
	t.Parallel()

	database := newDatabase(t)
	allow := permissions(func(context.Context, string, string, string) (auth.Decision, error) {
		return auth.Allowed, nil
	})
	client, stop, done := startApp(t, database, allow)
	user := bot.JID("51900000001@s.whatsapp.net")

	client.Deliver(bot.Message{Sender: lidAlice, User: user, Text: "hola"})
	waitForTurn(t, client, 0, 0)
	client.Deliver(bot.Message{Sender: lidAlice, User: user, Text: "!latex x"})
	waitForTurn(t, client, 1, 1)

	sent := client.Sent()
	if len(sent) != 2 || !strings.Contains(sent[0].Text, "Tinta") || sent[1].Text != "command handled" {
		t.Fatalf("sent = %+v, want welcome followed by command response", sent)
	}

	reactions := client.Reactions()
	if len(reactions) != 1 || reactions[0].Emoji != "👋" {
		t.Fatalf("reactions = %+v, want one welcome reaction", reactions)
	}

	stop()
	stopRuntime(t, done)

	client, stop, done = startApp(t, database, allow)
	client.Deliver(bot.Message{Sender: "lid:alice", User: user, Text: "hello"})
	waitForTurn(t, client, 0, 0)

	if got := client.Sent()[0].Text; got != "Welcome back! ✨ Use `!latex <equation>` to render math, or `!help` to see the menu." {
		t.Fatalf("persisted greeting = %q, want welcome back", got)
	}

	stop()
	stopRuntime(t, done)
}

func TestAppDoesNotWelcomeUnauthorizedGroups(t *testing.T) {
	t.Parallel()

	database := newDatabase(t)
	perms := permissions(func(_ context.Context, _, group, _ string) (auth.Decision, error) {
		if group != "" {
			return auth.GroupNotRegistered, nil
		}

		return auth.Allowed, nil
	})
	client, stop, done := startApp(t, database, perms)
	client.Deliver(bot.Message{
		Sender: "51900000001@s.whatsapp.net",
		User:   "51900000001@s.whatsapp.net",
		Chat:   "120363000000000000@g.us",
		Group:  true,
		Text:   "hola",
	})

	err := client.WaitForWork(t.Context())
	if err != nil {
		t.Fatalf("WaitForWork() = %v", err)
	}

	if len(client.Sent()) != 0 || len(client.Reactions()) != 0 {
		t.Fatalf("unauthorized group got replies: sent=%+v reactions=%+v", client.Sent(), client.Reactions())
	}

	stop()
	stopRuntime(t, done)
}

func stopRuntime(t *testing.T, done <-chan error) {
	t.Helper()

	err := <-done

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}
}

package main_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"maps"
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

	// help is a command every bot answers.
	help = "!help"
)

func environment(t *testing.T, vars map[string]string) *config.Env {
	t.Helper()

	if _, set := vars[config.KeyStore]; !set {
		vars[config.KeyStore] = filepath.Join(t.TempDir(), "bot.db")
	}

	return config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	})
}

func waitForTurn(t *testing.T, client *fake.Client) {
	t.Helper()

	err := client.WaitForWork(t.Context())
	if err != nil {
		t.Fatalf("WaitForWork() = %v", err)
	}
}

func stopRun(t *testing.T, stop context.CancelFunc, done <-chan error) {
	t.Helper()

	stop()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run() = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
}

func assertOfflineConversation(t *testing.T, client *fake.Client) {
	t.Helper()

	sent := client.Sent()
	if !strings.Contains(sent[0].Text, "Tinta") {
		t.Errorf("welcome = %q, want Tinta introduction", sent[0].Text)
	}

	if !strings.Contains(sent[1].Text, "Available Commands") {
		t.Errorf("help = %q, want command menu", sent[1].Text)
	}

	if sent[2].Image == nil || sent[2].Image.MIME != "image/png" {
		t.Errorf("rendered reply = %+v, want a PNG image", sent[2])
	}

	got := client.Reactions()
	if len(got) != 3 || got[0].Emoji != "👋" || got[1].Emoji != "✅" || got[2].Emoji != "✅" {
		t.Errorf("reactions = %+v, want welcome, success, success", got)
	}
}

func TestRunAnswersSeededOwnerInDirectChatsAndGroups(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	client := fake.New()
	env := environment(t, map[string]string{config.KeyOwners: string(owner)})
	done := make(chan error, 1)

	go func() {
		done <- latexcmd.Run(ctx, env, slog.New(slog.DiscardHandler), func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) {
			return client, nil
		})
	}()

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	client.Deliver(bot.Message{Sender: owner, Text: help})
	waitForTurn(t, client)

	// Groups reach the router, which refuses one nobody registered.
	client.Deliver(bot.Message{Sender: owner, Chat: group, Group: true, Text: help})
	waitForTurn(t, client)

	sent := client.Sent()
	if sent[0].To != owner {
		t.Errorf("help went to %q, want the owner's chat", sent[0].To)
	}

	if sent[1].To != group {
		t.Errorf("group notice went to %q, want the group", sent[1].To)
	}

	client.Deliver(bot.Message{Sender: owner, Chat: group, Group: true, Text: "hola"})

	beforeSent := len(client.Sent())
	beforeReactions := len(client.Reactions())
	waitForTurn(t, client)

	if len(client.Sent()) != beforeSent || len(client.Reactions()) != beforeReactions {
		t.Errorf("unauthorized group greeting activity: sent=%d reactions=%d, want sent=%d reactions=%d", len(client.Sent()), len(client.Reactions()), beforeSent, beforeReactions)
	}

	stop()

	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRunDrivesAnOfflineConversationThroughWelcomeMenuAndImage(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	client := fake.New()

	env := environment(t, map[string]string{config.KeyOwners: string(owner)})

	done := make(chan error, 1)

	go func() {
		done <- latexcmd.Run(ctx, env, slog.New(slog.DiscardHandler), func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) {
			return client, nil
		})
	}()

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	client.Deliver(bot.Message{Sender: owner, Text: "hola"})

	waitForTurn(t, client)
	client.Deliver(bot.Message{Sender: owner, Text: "!help"})

	waitForTurn(t, client)
	client.Deliver(bot.Message{Sender: owner, Text: `!latex \frac{a}{b}`})

	waitForTurn(t, client)

	assertOfflineConversation(t, client)
	stopRun(t, stop, done)
}

func TestRunAllowsFiveRequestsAMinute(t *testing.T) {
	t.Parallel()

	const allowed = 5

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	client := fake.New()
	env := environment(t, map[string]string{config.KeyOwners: string(owner)})
	done := make(chan error, 1)

	go func() {
		done <- latexcmd.Run(ctx, env, slog.New(slog.DiscardHandler), func(context.Context, *sql.DB, *slog.Logger) (bot.Transport, error) {
			return client, nil
		})
	}()

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for range allowed {
		client.Deliver(bot.Message{Sender: owner, Text: help})
		waitForTurn(t, client)
	}

	limited := client.Deliver(bot.Message{Sender: owner, Text: help})

	waitForTurn(t, client)
	stop()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}

	var warned []string

	for _, reaction := range client.Reactions() {
		if reaction.Emoji == "⚠️" {
			warned = append(warned, reaction.MessageID)
		}
	}

	if len(warned) != 1 || warned[0] != limited.ID {
		t.Errorf("messages warned about = %v, want only %s, the one past the limit", warned, limited.ID)
	}
}

func TestRunNamesEveryBadKey(t *testing.T) {
	t.Parallel()

	bad := map[string]string{config.KeyMaxInFlight: "many", latex.KeyTimeout: "soon"}

	err := latexcmd.Run(t.Context(), environment(t, maps.Clone(bad)), slog.New(slog.DiscardHandler), nil)
	if err == nil {
		t.Fatal("Run() succeeded on bad values")
	}

	for key := range bad {
		if !strings.Contains(err.Error(), key+"=") {
			t.Errorf("Run() error = %v, want it to name %s", err, key)
		}
	}
}

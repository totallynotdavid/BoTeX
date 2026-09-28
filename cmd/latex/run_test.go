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

func TestRunAnswersSeededOwnerInDirectChatsAndGroups(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	client := fake.New()
	env := environment(t, map[string]string{config.KeyOwners: string(owner)})
	done := make(chan error, 1)

	go func() {
		done <- latexcmd.Run(ctx, env, slog.New(slog.DiscardHandler), func(context.Context, *sql.DB, *slog.Logger) (bot.Client, error) {
			return client, nil
		})
	}()

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	client.Deliver(bot.Message{Sender: owner, Text: help})
	waitFor(t, func() bool { return len(client.Sent()) == 1 })

	// Groups reach the router, which refuses one nobody registered.
	client.Deliver(bot.Message{Sender: owner, Chat: group, Group: true, Text: help})
	waitFor(t, func() bool { return len(client.Sent()) == 2 })

	sent := client.Sent()
	if sent[0].To != owner {
		t.Errorf("help went to %q, want the owner's chat", sent[0].To)
	}

	if sent[1].To != group {
		t.Errorf("group notice went to %q, want the group", sent[1].To)
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

func TestRunAllowsFiveRequestsAMinute(t *testing.T) {
	t.Parallel()

	const allowed = 5

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	client := fake.New()
	env := environment(t, map[string]string{config.KeyOwners: string(owner)})
	done := make(chan error, 1)

	go func() {
		done <- latexcmd.Run(ctx, env, slog.New(slog.DiscardHandler), func(context.Context, *sql.DB, *slog.Logger) (bot.Client, error) {
			return client, nil
		})
	}()

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for i := range allowed {
		client.Deliver(bot.Message{Sender: owner, Text: help})
		waitFor(t, func() bool { return len(client.Sent()) == i+1 })
	}

	limited := client.Deliver(bot.Message{Sender: owner, Text: help})

	waitFor(t, func() bool { return len(client.Sent()) == allowed+1 })
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

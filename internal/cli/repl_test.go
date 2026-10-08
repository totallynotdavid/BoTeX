package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
)

type replApp func(context.Context, bot.Message, *bot.Chat) error

func (f replApp) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	return f(ctx, msg, chat)
}

func replCommand(app bot.App) cli.Command {
	return cli.Command{
		Name: "testbot",
		Configure: func(*config.Env) cli.Build {
			return func(context.Context, *sql.DB, *auth.Service, *slog.Logger) (cli.Built, error) {
				return cli.Built{App: app}, nil
			}
		},
	}
}

func replEnvironment(t *testing.T) *config.Env {
	t.Helper()

	return config.New(func(key string) (string, bool) {
		if key == config.KeyStore {
			return filepath.Join(t.TempDir(), "repl.db"), true
		}

		return "", false
	})
}

func runREPLTest(t *testing.T, app bot.App, input string, out *bytes.Buffer) {
	t.Helper()

	runREPLTestContext(t, t.Context(), app, input, out)
}

func runREPLTestContext(t *testing.T, ctx context.Context, app bot.App, input string, out *bytes.Buffer) {
	t.Helper()

	err := cli.RunREPL(ctx, replCommand(app), replEnvironment(t), slog.New(slog.DiscardHandler), strings.NewReader(input), out)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunREPLDrivesAConversationWithoutWhatsApp(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	app := replApp(func(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
		return chat.Send(ctx, "Echo: "+msg.Text)
	})
	runREPLTest(t, app, "hello\n:quit\n", &out)

	if !strings.Contains(out.String(), "bot> Echo: hello") {
		t.Fatalf("REPL output = %q, want the bot reply", out.String())
	}
}

func TestRunREPLReportsSilentTurnsWithoutWaitingForTheCeiling(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	app := replApp(func(context.Context, bot.Message, *bot.Chat) error { return nil })
	runREPLTestContext(t, ctx, app, "just chatting\n:quit\n", &out)

	if !strings.Contains(out.String(), "bot> (no reply)") {
		t.Fatalf("REPL output = %q, want a no-reply notice", out.String())
	}
}

func TestRunREPLWaitsForQuiescentMultiReplyTurns(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	app := replApp(func(ctx context.Context, _ bot.Message, chat *bot.Chat) error {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()

		<-timer.C

		err := chat.Send(ctx, "first reply")
		if err != nil {
			return fmt.Errorf("send first reply: %w", err)
		}

		return chat.Send(ctx, "follow-up reply")
	})
	runREPLTest(t, app, "hello\n:quit\n", &out)

	transcript := out.String()
	first := strings.Index(transcript, "bot> first reply")

	followUp := strings.Index(transcript, "bot> follow-up reply")
	if first < 0 || followUp < 0 || first > followUp {
		t.Fatalf("REPL output = %q, want both replies before the next prompt", transcript)
	}
}

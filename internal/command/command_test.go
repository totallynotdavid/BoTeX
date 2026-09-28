package command_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/command"
	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const (
	owner  bot.JID = "51900000001@s.whatsapp.net"
	member bot.JID = "51900000002@s.whatsapp.net"
	guest  bot.JID = "51900000003@s.whatsapp.net"

	wait = 5 * time.Second
)

var errBroken = errors.New("broken")

// echo replies with its arguments.
type echo struct{}

func (echo) Name() string { return "echo" }

func (echo) Info() command.Info {
	return command.Info{Description: "Repeat the text", Usage: "!echo <text>", Examples: []string{"!echo hi"}}
}

func (echo) Run(ctx context.Context, _ bot.Message, args string, chat *bot.Chat) error {
	err := chat.Send(ctx, args)
	if err != nil {
		return fmt.Errorf("echo: %w", err)
	}

	return nil
}

// broken always fails.
type broken struct{}

func (broken) Name() string       { return "broken" }
func (broken) Info() command.Info { return command.Info{Description: "Always fails", Usage: "!broken"} }

func (broken) Run(context.Context, bot.Message, string, *bot.Chat) error { return errBroken }

// handled collects the outcome of every message the router finished.
type handled struct {
	mu       sync.Mutex
	outcomes []error
	done     chan struct{}
}

func (h *handled) Record(_ context.Context, name string, attrs ...slog.Attr) {
	if name != "message_handled" {
		return
	}

	var err error

	for _, attr := range attrs {
		if attr.Key == "error" {
			recorded, isError := attr.Value.Any().(error)
			if isError {
				err = recorded
			}
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.outcomes = append(h.outcomes, err)
	h.done <- struct{}{}
}

// newService returns an auth service over a temporary database with one owner
// and one member registered.
func newService(t *testing.T) *auth.Service {
	t.Helper()

	service, _ := newServiceAndDatabase(t)

	return service
}

func newServiceAndDatabase(t *testing.T) (*auth.Service, *sql.DB) {
	t.Helper()

	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	service, err := auth.New(t.Context(), database, auth.Rank{Name: "member", Level: 100, Commands: []string{"help", "echo", "broken"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.SeedOwners(t.Context(), []string{string(owner)})
	if err != nil {
		t.Fatal(err)
	}

	err = service.RegisterUser(t.Context(), string(member), "member", "test")
	if err != nil {
		t.Fatal(err)
	}

	return service, database
}

// runBot runs the router over service behind a bot on a fake client until the
// test ends.
func runBot(t *testing.T, service *auth.Service, inFlight int, recorder bot.Recorder) *fake.Client {
	t.Helper()

	client := fake.New()
	router := command.NewRouter("!", service, echo{}, broken{})
	runtime := bot.New(client, router, slog.New(slog.DiscardHandler), bot.Options{Recorder: recorder, MaxInFlight: inFlight})

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})

	var runErr error

	go func() {
		defer close(stopped)

		runErr = runtime.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-stopped

		if !errors.Is(runErr, context.Canceled) {
			t.Errorf("Run() = %v, want context canceled", runErr)
		}
	})

	err := client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

// chat delivers one message per text from sender to the router and returns the
// client and each message's outcome once the router has finished with all of
// them.
func chat(t *testing.T, sender bot.JID, texts ...string) (*fake.Client, []error) {
	t.Helper()

	return chatOver(t, newService(t), sender, texts...)
}

// chatOver is chat with the router checking permissions in service.
func chatOver(t *testing.T, service *auth.Service, sender bot.JID, texts ...string) (*fake.Client, []error) {
	t.Helper()

	results := &handled{done: make(chan struct{}, len(texts))}
	client := runBot(t, service, len(texts), results)

	for _, text := range texts {
		client.Deliver(bot.Message{Sender: sender, Text: text})
	}

	for range texts {
		select {
		case <-results.done:
		case <-time.After(wait):
			t.Fatal("the router did not finish every message")
		}
	}

	results.mu.Lock()
	defer results.mu.Unlock()

	return client, slices.Clone(results.outcomes)
}

func texts(sent []fake.Sent) []string {
	out := make([]string, 0, len(sent))
	for _, s := range sent {
		out = append(out, s.Text)
	}

	return out
}

func emojis(reactions []fake.Reaction) []string {
	out := make([]string, 0, len(reactions))
	for _, r := range reactions {
		out = append(out, r.Emoji)
	}

	return out
}

func TestRunsAllowedCommandWithArgs(t *testing.T) {
	t.Parallel()

	client, outcomes := chat(t, member, "!echo  hello\nworld  ")

	if outcomes[0] != nil {
		t.Fatalf("handler failed: %v", outcomes[0])
	}

	if got := texts(client.Sent()); !slices.Equal(got, []string{"hello\nworld"}) {
		t.Errorf("sent %q, want the trimmed arguments with their line break", got)
	}

	if got := emojis(client.Reactions()); !slices.Equal(got, []string{"✅"}) {
		t.Errorf("reactions = %v, want ✅", got)
	}
}

func TestRankWithoutTheCommandIsDenied(t *testing.T) {
	t.Parallel()

	client, outcomes := chat(t, member, "!admin now")

	if outcomes[0] != nil {
		t.Fatalf("handler failed: %v", outcomes[0])
	}

	if got := emojis(client.Reactions()); !slices.Equal(got, []string{"🚫"}) {
		t.Errorf("reactions = %v, want 🚫", got)
	}

	if got := texts(client.Sent()); len(got) != 1 || !strings.Contains(got[0], "`!admin`") {
		t.Errorf("sent %q, want a notice naming the command", got)
	}
}

func TestUnregisteredUserIsDeniedEveryCommand(t *testing.T) {
	t.Parallel()

	client, _ := chat(t, guest, "!echo hi", "!nothing")

	if got := emojis(client.Reactions()); !slices.Equal(got, []string{"🚫", "🚫"}) {
		t.Errorf("reactions = %v, want only 🚫", got)
	}

	for _, sent := range client.Sent() {
		if sent.Text == "hi" {
			t.Error("an unregistered user ran a command")
		}
	}
}

func TestUnknownCommandFromAllowedUserFails(t *testing.T) {
	t.Parallel()

	client, outcomes := chat(t, owner, "!nothing")

	if outcomes[0] != nil {
		t.Fatalf("handler failed: %v", outcomes[0])
	}

	if got := emojis(client.Reactions()); !slices.Equal(got, []string{"❌"}) {
		t.Errorf("reactions = %v, want ❌", got)
	}

	if got := texts(client.Sent()); len(got) != 1 || !strings.Contains(got[0], "`!nothing` not found") {
		t.Errorf("sent %q, want a not-found notice", got)
	}
}

func TestFailingCommandReactsAndReportsTheError(t *testing.T) {
	t.Parallel()

	client, outcomes := chat(t, member, "!broken")

	if !errors.Is(outcomes[0], errBroken) {
		t.Errorf("handler outcome = %v, want the command's error", outcomes[0])
	}

	if got := emojis(client.Reactions()); !slices.Equal(got, []string{"❌"}) {
		t.Errorf("reactions = %v, want ❌", got)
	}
}

func TestPermissionErrorIsReported(t *testing.T) {
	t.Parallel()

	service, database := newServiceAndDatabase(t)

	err := database.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, outcomes := chatOver(t, service, member, "!echo hi")

	if outcomes[0] == nil || !strings.Contains(outcomes[0].Error(), "check permission") {
		t.Errorf("handler outcome = %v, want the permission check's error", outcomes[0])
	}
}

func TestNameThatCannotBeACommandIsUnknown(t *testing.T) {
	t.Parallel()

	client, outcomes := chat(t, member, "!héllo", "!!", "!"+strings.Repeat("a", 51))

	for _, outcome := range outcomes {
		if outcome != nil {
			t.Errorf("handler failed: %v", outcome)
		}
	}

	if got := emojis(client.Reactions()); !slices.Equal(got, []string{"❌", "❌", "❌"}) {
		t.Errorf("reactions = %v, want ❌ for each", got)
	}

	for _, sent := range texts(client.Sent()) {
		if !strings.Contains(sent, "not found") {
			t.Errorf("sent %q, want a not-found notice", sent)
		}
	}

	if len(client.Sent()) != 3 {
		t.Errorf("sent %d notices, want 3", len(client.Sent()))
	}
}

func TestMessagesWithoutThePrefixAreIgnored(t *testing.T) {
	t.Parallel()

	client, outcomes := chat(t, member, "hello", "echo hi", "!", "  ! ")

	for _, outcome := range outcomes {
		if outcome != nil {
			t.Errorf("handler failed: %v", outcome)
		}
	}

	if len(client.Reactions()) != 0 || len(client.Sent()) != 0 {
		t.Errorf("router replied: reactions %v, sent %v", client.Reactions(), client.Sent())
	}
}

func TestHelpListsCommandsInOrderWithoutItself(t *testing.T) {
	t.Parallel()

	client, _ := chat(t, member, "!help")

	want := "*Available Commands*\n\n" +
		"• *echo* - Repeat the text\n" +
		"• *broken* - Always fails\n" +
		"\nUse `!help <command>` for detailed usage."
	if got := texts(client.Sent()); !slices.Equal(got, []string{want}) {
		t.Errorf("help sent %q, want %q", got, want)
	}
}

func TestHelpDescribesOneCommand(t *testing.T) {
	t.Parallel()

	client, _ := chat(t, member, "!help echo extra", "!help help", "!help nope")

	sent := texts(client.Sent())
	slices.Sort(sent)

	wantEcho := "*echo Command*\n\nRepeat the text\n\n*Usage:* `!echo <text>`\n\n*Examples:*\n`!echo hi`\n"

	if !slices.Contains(sent, wantEcho) {
		t.Errorf("no description of echo in %q", sent)
	}

	if !slices.ContainsFunc(sent, func(s string) bool {
		return strings.HasPrefix(s, "*help Command*") && strings.Contains(s, "`!help [command]`")
	}) {
		t.Errorf("no description of help in %q", sent)
	}

	if !slices.ContainsFunc(sent, func(s string) bool { return strings.Contains(s, "`nope` not found") }) {
		t.Errorf("no not-found notice in %q", sent)
	}
}

func TestHelpNamesTheCommandUpToTheFirstWhitespace(t *testing.T) {
	t.Parallel()

	client, _ := chat(t, member, "!help echo\nextra", "!help echo\textra")

	for _, sent := range texts(client.Sent()) {
		if !strings.HasPrefix(sent, "*echo Command*") {
			t.Errorf("help sent %q, want the description of echo", sent)
		}
	}

	if len(client.Sent()) != 2 {
		t.Errorf("sent %d messages, want 2", len(client.Sent()))
	}
}

func TestNewRouterPanicsOnDuplicateNames(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewRouter accepted two commands named echo")
		}
	}()

	command.NewRouter("!", nil, echo{}, echo{})
}

func TestCommandNamedHelpIsRejected(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewRouter accepted a command named help")
		}
	}()

	command.NewRouter("!", nil, helpImpostor{})
}

type helpImpostor struct{ broken }

func (helpImpostor) Name() string { return "help" }

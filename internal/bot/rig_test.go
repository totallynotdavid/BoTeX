package bot_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const (
	alice bot.JID = "51900000001@s.whatsapp.net"
	bob   bot.JID = "51900000002@s.whatsapp.net"
	group bot.JID = "120363000000000000@g.us"

	wait = 5 * time.Second
)

type appFunc func(ctx context.Context, m bot.Message, c *bot.Chat) error

func (f appFunc) Handle(ctx context.Context, m bot.Message, c *bot.Chat) error { return f(ctx, m, c) }

// event is one Recorder call.
type event struct {
	name  string
	attrs map[string]slog.Value
}

func (e event) reason() string { return e.attrs["reason"].String() }

type recorder struct {
	mu     sync.Mutex
	events []event
}

func (r *recorder) Record(_ context.Context, name string, attrs ...slog.Attr) {
	values := make(map[string]slog.Value, len(attrs))
	for _, attr := range attrs {
		values[attr.Key] = attr.Value
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, event{name: name, attrs: values})
}

func (r *recorder) named(name string) []event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.DeleteFunc(slices.Clone(r.events), func(e event) bool { return e.name != name })
}

// reasons lists the reason of every message_dropped event, in order.
func (r *recorder) reasons() []string {
	dropped := r.named("message_dropped")
	reasons := make([]string, 0, len(dropped))

	for _, e := range dropped {
		reasons = append(reasons, e.reason())
	}

	return reasons
}

// logs collects log records.
type logs struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*logs) Enabled(context.Context, slog.Level) bool { return true }
func (l *logs) WithAttrs([]slog.Attr) slog.Handler     { return l }
func (l *logs) WithGroup(string) slog.Handler          { return l }

func (l *logs) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.records = append(l.records, r)

	return nil
}

func (l *logs) at(level slog.Level) []slog.Record {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.DeleteFunc(slices.Clone(l.records), func(r slog.Record) bool { return r.Level != level })
}

// attr returns the value of the named attribute of r.
func attr(r slog.Record, key string) string {
	var value string

	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			value = a.Value.String()
		}

		return a.Key != key
	})

	return value
}

// rig runs a Bot over a paired fake client.
type rig struct {
	client *fake.Client
	rec    *recorder
	logs   *logs
	bot    *bot.Bot

	stopped chan struct{}
	err     error // Run's result, set before stopped is closed
}

// start runs a Bot with app until the test ends and waits for it to connect.
func start(t *testing.T, app bot.App, opts bot.Options) *rig {
	t.Helper()

	env := &rig{client: fake.New(), rec: &recorder{}, logs: &logs{}, stopped: make(chan struct{})}
	opts.Recorder = env.rec
	env.bot = bot.New(env.client, app, slog.New(env.logs), opts)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		env.wait(t)
	})

	go func() {
		defer close(env.stopped)

		env.err = env.bot.Run(ctx)
	}()

	waiting, stop := context.WithTimeout(t.Context(), wait)
	defer stop()

	err := env.client.WaitConnected(waiting)
	if err != nil {
		t.Fatalf("bot never connected: %v", err)
	}

	return env
}

// wait blocks until Run has returned.
func (r *rig) wait(t *testing.T) {
	t.Helper()

	select {
	case <-r.stopped:
	case <-time.After(wait):
		t.Fatal("Run did not return")
	}
}

// result waits for Run to return and reports what it returned.
func (r *rig) result(t *testing.T) error {
	t.Helper()

	r.wait(t)

	return r.err
}

// eventually polls cond until it holds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(time.Millisecond)
	}
}

func direct(from bot.JID, text string) bot.Message {
	return bot.Message{Sender: from, Text: text}
}

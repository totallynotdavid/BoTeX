package bot_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

// ended is what a handler saw once its context was cancelled.
type ended struct {
	cause    error
	sendErr  error
	imageErr error
}

func idle(context.Context, bot.Message, *bot.Chat) error { return nil }

func TestEndSessionStopsRunWithSessionEndedError(t *testing.T) {
	t.Parallel()

	env := start(t, appFunc(idle), bot.Options{})

	env.client.EndSession(bot.LoggedOut, "401")

	err := env.result(t)

	var got *bot.SessionEndedError
	if !errors.As(err, &got) {
		t.Fatalf("Run() = %v, want *SessionEndedError", err)
	}

	if got.Reason != bot.LoggedOut || got.Detail != "401" {
		t.Errorf("Run() ended with %+v, want logged_out 401", got)
	}

	if env.client.Connected() {
		t.Error("client still connected after Run returned")
	}
}

func TestSessionEndLogsReasonDetailAndRemedy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason bot.SessionEndReason
		remedy string
	}{
		{bot.LoggedOut, "pair"},
		{bot.Replaced, "stop it"},
		{bot.Banned, "ban expires"},
		{bot.Outdated, "update"},
	}

	for _, scenario := range cases {
		t.Run(scenario.reason.String(), func(t *testing.T) {
			t.Parallel()

			env := start(t, appFunc(idle), bot.Options{})

			env.client.EndSession(scenario.reason, "detail-x")
			env.wait(t)

			records := env.logs.at(slog.LevelError)
			if len(records) != 1 {
				t.Fatalf("got %d ERROR records, want 1", len(records))
			}

			record := records[0]
			if attr(record, "reason") != scenario.reason.String() || attr(record, "detail") != "detail-x" ||
				!strings.Contains(attr(record, "remedy"), scenario.remedy) {
				t.Errorf("ERROR record %q lacks reason, detail or remedy %q", record.Message, scenario.remedy)
			}
		})
	}
}

func TestSessionEndedRecordedOnce(t *testing.T) {
	t.Parallel()

	env := start(t, appFunc(idle), bot.Options{})

	env.client.EndSession(bot.Replaced, "")
	env.client.EndSession(bot.LoggedOut, "second")
	env.wait(t)

	events := env.rec.named("session_ended")
	if len(events) != 1 {
		t.Fatalf("recorded session_ended %d times, want 1", len(events))
	}

	if events[0].reason() != "replaced" {
		t.Errorf("session_ended reason = %q, want the first end's", events[0].reason())
	}

	if got := len(env.logs.at(slog.LevelError)); got != 1 {
		t.Errorf("logged %d ERROR records, want 1", got)
	}
}

func TestSessionEndReleasesBlockedHandlersWithCause(t *testing.T) {
	t.Parallel()

	const handlers = 3

	started := make(chan struct{}, handlers)
	seen := make(chan ended, handlers)

	app := appFunc(func(ctx context.Context, _ bot.Message, chat *bot.Chat) error {
		started <- struct{}{}

		<-ctx.Done()

		seen <- ended{
			cause:    context.Cause(ctx),
			sendErr:  chat.Send(context.WithoutCancel(ctx), "late"),
			imageErr: chat.SendImage(context.WithoutCancel(ctx), bot.Image{}),
		}

		return context.Cause(ctx)
	})

	env := start(t, app, bot.Options{})

	for _, from := range []bot.JID{alice, bob, "51900000003@s.whatsapp.net"} {
		env.client.Deliver(direct(from, "!wait"))
	}

	for range handlers {
		<-started
	}

	env.client.EndSession(bot.LoggedOut, "")

	want := env.result(t)

	for range handlers {
		got := <-seen
		if !errors.Is(got.cause, want) || !errors.Is(got.sendErr, want) || !errors.Is(got.imageErr, want) {
			t.Errorf("handler saw cause=%v send=%v image=%v, want %v for all", got.cause, got.sendErr, got.imageErr, want)
		}
	}

	if sent := env.client.Sent(); len(sent) != 0 {
		t.Errorf("sent %v after the session ended", sent)
	}

	// The handlers' own return is the cause, which is not a handler failure.
	if got := len(env.logs.at(slog.LevelError)); got != 1 {
		t.Errorf("logged %d ERROR records, want only the session end", got)
	}
}

func TestMessageAfterSessionEndNeverReachesApp(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{}, 2)
	delivered := make(chan string, 2)

	app := appFunc(func(_ context.Context, msg bot.Message, _ *bot.Chat) error {
		delivered <- msg.Text

		started <- struct{}{}

		<-release // ignores ctx, so Run stays in its grace period

		return nil
	})

	env := start(t, app, bot.Options{})

	env.client.Deliver(direct(alice, "before"))
	<-started

	env.client.EndSession(bot.LoggedOut, "")
	env.client.Deliver(direct(bob, "after"))

	if reasons := env.rec.reasons(); len(reasons) != 1 || reasons[0] != "session_ended" {
		t.Errorf("dropped for %v, want [session_ended]", reasons)
	}

	close(release)
	env.wait(t)

	if len(delivered) != 1 || <-delivered != "before" {
		t.Errorf("app saw %d messages, want only the one before the end", len(delivered)+1)
	}
}

func TestRunWaitsOnlyForTheGracePeriod(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{})

	app := appFunc(func(context.Context, bot.Message, *bot.Chat) error {
		close(started)
		<-release

		return nil
	})

	env := start(t, app, bot.Options{})
	t.Cleanup(func() { close(release) })

	env.bot.SetGrace(20 * time.Millisecond)
	env.client.Deliver(direct(alice, "!stuck"))
	<-started
	env.client.EndSession(bot.LoggedOut, "")

	err := env.result(t)

	end, isEnd := errors.AsType[*bot.SessionEndedError](err)
	if !isEnd || end.Reason != bot.LoggedOut {
		t.Fatalf("Run() = %v, want *SessionEndedError with reason logged_out", err)
	}

	if len(env.logs.at(slog.LevelWarn)) == 0 {
		t.Error("no WARN record for the handler that outlived the grace period")
	}
}

func TestUnpairedRunReturnsNotPairedUnchanged(t *testing.T) {
	t.Parallel()

	client := fake.NewUnpaired()
	rec := &recorder{}
	sink := &logs{}

	called := false
	app := appFunc(func(context.Context, bot.Message, *bot.Chat) error {
		called = true

		return nil
	})

	err := bot.New(client, app, slog.New(sink), bot.Options{Recorder: rec}).Run(t.Context())

	var got *bot.SessionEndedError
	if !errors.As(err, &got) || got.Reason != bot.NotPaired {
		t.Fatalf("Run() = %#v, want the client's *SessionEndedError with reason not_paired", err)
	}

	//nolint:err113,errorlint // identity is the point: Run must not wrap the client's error.
	if err != error(got) {
		t.Errorf("Run() wrapped the client's error: %v", err)
	}

	if called {
		t.Error("app ran on an unpaired client")
	}

	records := sink.at(slog.LevelError)
	if len(records) != 1 || !strings.Contains(attr(records[0], "remedy"), "pair") {
		t.Errorf("ERROR records %v do not name the pair command", records)
	}

	if got := len(rec.named("session_ended")); got != 1 {
		t.Errorf("recorded session_ended %d times, want 1", got)
	}
}

func TestCancelledContextAbortsAHangingConnect(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	ctx, cancel := context.WithCancel(t.Context())
	_, result := runStalled(t, ctx, rec, &logs{})

	select {
	case err := <-result:
		t.Fatalf("Run() returned %v while Connect was hanging", err)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-result:
		if err != context.Canceled { //nolint:errorlint,err113 // Run documents that it returns the context's error itself, not a wrapper.
			t.Errorf("Run() = %v, want context.Canceled itself", err)
		}
	case <-time.After(wait):
		t.Fatal("Run did not return after its context was cancelled")
	}

	if got := len(rec.named("session_ended")); got != 0 {
		t.Errorf("recorded session_ended %d times for a caller cancel, want 0", got)
	}
}

// runStalled starts Run on a client whose Connect hangs and returns once the
// dial is waiting.
func runStalled(t *testing.T, ctx context.Context, rec *recorder, sink *logs) (client *fake.Client, result <-chan error) {
	t.Helper()

	client = fake.New()
	client.StallConnect()

	done := make(chan error, 1)

	go func() {
		done <- bot.New(client, appFunc(nil), slog.New(sink), bot.Options{Recorder: rec}).Run(ctx)
	}()

	err := client.WaitDialing(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return client, done
}

func TestSessionEndDuringABlockedConnectStopsRun(t *testing.T) {
	t.Parallel()

	rec, sink := &recorder{}, &logs{}
	client, result := runStalled(t, t.Context(), rec, sink)

	client.EndSession(bot.LoggedOut, "401")

	select {
	case err := <-result:
		end, ok := errors.AsType[*bot.SessionEndedError](err)
		if !ok || end.Reason != bot.LoggedOut || end.Detail != "401" {
			t.Fatalf("Run() = %v, want the *SessionEndedError logged_out 401", err)
		}
	case <-time.After(wait):
		t.Fatal("Run kept waiting for a Connect that a session end should have aborted")
	}

	if got := len(rec.named("session_ended")); got != 1 {
		t.Errorf("recorded session_ended %d times, want 1", got)
	}

	records := sink.at(slog.LevelError)
	if len(records) != 1 || !strings.Contains(attr(records[0], "remedy"), "pair") {
		t.Errorf("ERROR records %v do not name the pair command", records)
	}
}

func TestSessionEndAndCancelDuringABlockedConnectReturnTheSessionEnd(t *testing.T) {
	t.Parallel()

	rec, sink := &recorder{}, &logs{}
	ctx, cancel := context.WithCancel(t.Context())
	client, result := runStalled(t, ctx, rec, sink)

	client.EndSession(bot.Replaced, "")
	cancel()

	select {
	case err := <-result:
		end, ok := errors.AsType[*bot.SessionEndedError](err)
		if !ok || end.Reason != bot.Replaced {
			t.Fatalf("Run() = %v, want the *SessionEndedError replaced", err)
		}
	case <-time.After(wait):
		t.Fatal("Run did not return")
	}

	if got := len(rec.named("session_ended")); got != 1 {
		t.Errorf("recorded session_ended %d times, want 1", got)
	}
}

func TestSessionEndWinsOverACancelledContext(t *testing.T) {
	t.Parallel()

	// Run's wait sees both the end and the cancelled context ready and picks
	// either at random, so one pass proves little.
	for attempt := range 50 {
		client := fake.New()
		client.EmitOnConnect(bot.SessionEnded{Reason: bot.LoggedOut, Detail: "401"})

		rec, sink := &recorder{}, &logs{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := bot.New(client, appFunc(nil), slog.New(sink), bot.Options{Recorder: rec}).Run(ctx)

		end, ok := errors.AsType[*bot.SessionEndedError](err)
		if !ok || end.Reason != bot.LoggedOut {
			t.Fatalf("attempt %d: Run() = %v, want the *SessionEndedError that was pending", attempt, err)
		}

		records := sink.at(slog.LevelError)
		if len(records) != 1 || !strings.Contains(attr(records[0], "remedy"), "pair") {
			t.Fatalf("attempt %d: ERROR records %v do not name the pair command", attempt, records)
		}

		if got := len(rec.named("session_ended")); got != 1 {
			t.Fatalf("attempt %d: recorded session_ended %d times, want 1", attempt, got)
		}
	}
}

func TestCancelledContextStopsRunAndHandlers(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	cause := make(chan error, 1)

	app := appFunc(func(ctx context.Context, _ bot.Message, _ *bot.Chat) error {
		close(started)
		<-ctx.Done()

		cause <- context.Cause(ctx)

		return nil
	})

	client := fake.New()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)

	go func() { result <- bot.New(client, app, slog.New(&logs{}), bot.Options{}).Run(ctx) }()

	err := client.WaitConnected(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	client.Deliver(direct(alice, "!wait"))
	<-started
	cancel()

	err = <-result
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context canceled", err)
	}

	got := <-cause
	if !errors.Is(got, context.Canceled) {
		t.Errorf("handler saw cause %v, want context canceled", got)
	}

	if client.Connected() {
		t.Error("client still connected after Run returned")
	}
}

func TestDisconnectedIsLoggedAndRunGoesOn(t *testing.T) {
	t.Parallel()

	handled := make(chan struct{})
	env := start(t, appFunc(func(context.Context, bot.Message, *bot.Chat) error {
		close(handled)

		return nil
	}), bot.Options{})

	env.client.Emit(bot.Disconnected{})
	env.client.Emit(bot.Connected{})
	env.client.Deliver(direct(alice, "!hi"))
	<-handled

	warnings, disconnects := len(env.logs.at(slog.LevelWarn)), len(env.rec.named("disconnected"))
	if warnings != 1 || disconnects != 1 {
		t.Errorf("disconnect logged %d WARN records and recorded %d events, want 1 each", warnings, disconnects)
	}

	if len(env.rec.named("connected")) != 1 {
		t.Error("connected was not recorded")
	}
}

func TestRunTwiceFails(t *testing.T) {
	t.Parallel()

	env := start(t, appFunc(nil), bot.Options{})

	err := env.bot.Run(t.Context())
	if !errors.Is(err, bot.ErrRunTwice) {
		t.Fatalf("second Run() = %v, want ErrRunTwice", err)
	}
}

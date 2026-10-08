package bot_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/ratelimit"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

var errBoom = errors.New("boom")

// count is an app that counts the messages it handles.
func count(n *atomic.Int32) appFunc {
	return func(context.Context, bot.Message, *bot.Chat) error {
		n.Add(1)

		return nil
	}
}

func TestFiltersDropWhatTheOptionsExclude(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		opts   bot.Options
		msg    bot.Message
		reason string // "" when the app must get the message
	}{
		{"own message by default", bot.Options{}, bot.Message{Sender: fake.OwnJID, FromMe: true}, "from_me"},
		{"own message when enabled", bot.Options{OwnMessages: true}, bot.Message{Sender: fake.OwnJID, FromMe: true}, ""},
		{"group by default", bot.Options{}, bot.Message{Sender: alice, Chat: group, Group: true}, "group"},
		{"group when enabled", bot.Options{Groups: true}, bot.Message{Sender: alice, Chat: group, Group: true}, ""},
		{"sender outside allow list", bot.Options{AllowOnly: []bot.JID{bob}}, bot.Message{Sender: alice}, "not_allowed"},
		{"sender on allow list", bot.Options{AllowOnly: []bot.JID{bob}}, bot.Message{Sender: bob}, ""},
		{"anyone when the allow list is empty", bot.Options{}, bot.Message{Sender: alice}, ""},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			var handled atomic.Int32

			env := start(t, count(&handled), scenario.opts)
			env.client.Deliver(scenario.msg)

			if got := len(env.rec.named("message_received")); got != 1 {
				t.Errorf("message_received recorded %d times, want 1", got)
			}

			if scenario.reason == "" {
				env.expectHandled(t)
			} else {
				env.expectDropped(t, scenario.reason, &handled)
			}
		})
	}
}

// expectHandled checks that the one delivered message reached the app.
func (r *rig) expectHandled(t *testing.T) {
	t.Helper()

	eventually(t, "message_handled", func() bool { return len(r.rec.named("message_handled")) == 1 })

	if reasons := r.rec.reasons(); len(reasons) != 0 {
		t.Errorf("accepted message was also dropped: %v", reasons)
	}
}

// expectDropped checks that the one delivered message was dropped for reason
// and never reached the app.
func (r *rig) expectDropped(t *testing.T, reason string, handled *atomic.Int32) {
	t.Helper()

	// Drops are recorded on the delivering goroutine, so no waiting.
	if reasons := r.rec.reasons(); !slices.Equal(reasons, []string{reason}) {
		t.Errorf("dropped for %v, want [%s]", reasons, reason)
	}

	if handled.Load() != 0 || len(r.rec.named("message_handled")) != 0 {
		t.Error("dropped message reached the app")
	}
}

func TestFiltersApplyInOrder(t *testing.T) {
	t.Parallel()

	var handled atomic.Int32

	env := start(t, count(&handled), bot.Options{AllowOnly: []bot.JID{bob}})

	// Own, in a group and off the allow list: the first step names the drop.
	env.client.Deliver(bot.Message{Sender: alice, Chat: group, Group: true, FromMe: true})
	env.client.Deliver(bot.Message{Sender: alice, Chat: group, Group: true})

	if reasons := env.rec.reasons(); !slices.Equal(reasons, []string{"from_me", "group"}) {
		t.Errorf("dropped for %v, want [from_me group]", reasons)
	}
}

func TestRateLimitReactsAndNotifiesOncePerWindow(t *testing.T) {
	t.Parallel()

	var handled atomic.Int32

	limiter, err := ratelimit.NewLimiter(1, time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	env := start(t, count(&handled), bot.Options{Limiter: limiter})

	env.client.Deliver(direct(alice, "!one"))
	second := env.client.Deliver(direct(alice, "!two"))
	third := env.client.Deliver(direct(alice, "!three"))
	env.client.Deliver(direct(bob, "!four")) // another user has their own allowance

	eventually(t, "replies to the limited messages", func() bool {
		return len(env.client.Reactions()) == 2 && len(env.client.Sent()) == 1
	})
	eventually(t, "both allowed messages", func() bool { return handled.Load() == 2 })

	if reasons := env.rec.reasons(); !slices.Equal(reasons, []string{reasonRateLimited, reasonRateLimited}) {
		t.Errorf("dropped for %v, want two rate_limited", reasons)
	}

	want := []fake.Reaction{
		{Chat: alice, MessageID: second.ID, Emoji: "⚠️"},
		{Chat: alice, MessageID: third.ID, Emoji: "⚠️"},
	}
	if reactions := env.client.Reactions(); !slices.Equal(sorted(reactions), sorted(want)) {
		t.Errorf("reactions = %+v, want %+v", reactions, want)
	}

	notice := env.client.Sent()[0]
	if notice.To != alice || notice.Text != "Too many requests. Please wait 3600 seconds." {
		t.Errorf("notice = %+v", notice)
	}
}

func sorted(reactions []fake.Reaction) []fake.Reaction {
	reactions = slices.Clone(reactions)
	slices.SortFunc(reactions, func(a, b fake.Reaction) int {
		if a.MessageID < b.MessageID {
			return -1
		}

		return 1
	})

	return reactions
}

// gauge tracks how many handlers run at once.
type gauge struct {
	running, peak atomic.Int32
}

func (g *gauge) enter() {
	now := g.running.Add(1)

	for {
		seen := g.peak.Load()
		if now <= seen || g.peak.CompareAndSwap(seen, now) {
			return
		}
	}
}

func (g *gauge) exit() { g.running.Add(-1) }

func TestMaxInFlightHolds(t *testing.T) {
	t.Parallel()

	const (
		limit    = 2
		messages = limit + bot.RefusalSlots // every refusal fits its bound and is answered
	)

	var (
		load    gauge
		handled atomic.Int32
	)

	release := make(chan struct{})

	app := appFunc(func(ctx context.Context, _ bot.Message, _ *bot.Chat) error {
		load.enter()
		defer load.exit()

		select {
		case <-release:
		case <-ctx.Done():
		}

		handled.Add(1)

		return nil
	})

	env := start(t, app, bot.Options{MaxInFlight: limit})

	for i := range messages {
		env.client.Deliver(direct(bot.JID(fmt.Sprintf("519000010%02d@s.whatsapp.net", i)), "!work"))
	}

	eventually(t, "the slots to fill", func() bool { return load.running.Load() == limit })

	reasons := env.rec.reasons()
	if len(reasons) != messages-limit || slices.ContainsFunc(reasons, func(s string) bool { return s != reasonBusy }) {
		t.Errorf("dropped for %v, want %d busy", reasons, messages-limit)
	}

	eventually(t, "a reaction and a notice per refused message", func() bool {
		return len(env.client.Reactions()) == messages-limit && len(env.client.Sent()) == messages-limit
	})

	close(release)
	eventually(t, "the held handlers", func() bool { return handled.Load() == limit })

	// Freed slots take new messages.
	env.client.Deliver(direct(alice, "!again"))
	eventually(t, "a message after the slots freed", func() bool { return handled.Load() == limit+1 })

	if got := load.peak.Load(); got > limit {
		t.Errorf("%d handlers ran at once, limit %d", got, limit)
	}
}

// blockedReact is a Transport whose React holds until release is closed, and
// which counts how many Reacts are held at once.
type blockedReact struct {
	bot.Transport

	release chan struct{}
	load    gauge
}

func (c *blockedReact) React(ctx context.Context, msg bot.Message, emoji string) error {
	c.load.enter()
	defer c.load.exit()

	<-c.release

	err := c.Transport.React(ctx, msg, emoji)
	if err != nil {
		return fmt.Errorf("blocked reaction: %w", err)
	}

	return nil
}

func TestRefusalRepliesAreBounded(t *testing.T) {
	t.Parallel()

	const (
		limit   = 2
		refused = bot.RefusalSlots + 6
	)

	cases := []struct {
		name   string
		opts   bot.Options
		reason string
		// handled is how many messages reach the app, so how many are not refused.
		handled int
		// sender is who sends message i.
		sender func(i int) bot.JID
	}{
		{
			name:    "busy",
			opts:    bot.Options{MaxInFlight: limit},
			reason:  reasonBusy,
			handled: limit,
			sender:  func(i int) bot.JID { return bot.JID(fmt.Sprintf("519000010%02d@s.whatsapp.net", i)) },
		},
		{
			name:    "rate limited",
			opts:    bot.Options{Limiter: hourlyLimiter(t, 1)},
			reason:  reasonRateLimited,
			handled: 1,
			sender:  func(int) bot.JID { return alice },
		},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			release := make(chan struct{})
			app := appFunc(func(context.Context, bot.Message, *bot.Chat) error {
				<-release

				return nil
			})

			var held *blockedReact

			env := startWrapped(t, app, scenario.opts, func(client *fake.Client) bot.Transport {
				held = &blockedReact{Transport: client, release: release}

				return held
			})

			for i := range scenario.handled + refused {
				env.client.Deliver(direct(scenario.sender(i), "!flood"))
			}

			// The refusals beyond the bound are decided on the delivering goroutine.
			reasons := env.rec.reasons()
			if len(reasons) != refused || slices.ContainsFunc(reasons, func(s string) bool { return s != scenario.reason }) {
				t.Errorf("dropped for %v, want %d %s", reasons, refused, scenario.reason)
			}

			eventually(t, "the refusal replies to fill", func() bool { return held.load.running.Load() >= bot.RefusalSlots })

			// A reply that was refused a place would show up as one more Reacts
			// in flight. Give it the chance to.
			time.Sleep(50 * time.Millisecond)

			if got := held.load.peak.Load(); got != bot.RefusalSlots {
				t.Errorf("%d refusal replies ran at once, want %d", got, bot.RefusalSlots)
			}

			env.stop()
			close(release)

			err := env.result(t)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Run() error = %v, want context.Canceled", err)
			}
		})
	}
}

func hourlyLimiter(t *testing.T, requests int) *ratelimit.Limiter {
	t.Helper()

	limiter, err := ratelimit.NewLimiter(requests, time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	return limiter
}

func TestHandlerOutcomeIsRecorded(t *testing.T) {
	t.Parallel()

	app := appFunc(func(_ context.Context, m bot.Message, _ *bot.Chat) error {
		switch m.Text {
		case "!fail":
			return errBoom
		case "!panic":
			panic("kaboom")
		default:
			return nil
		}
	})

	env := start(t, app, bot.Options{})

	for _, text := range []string{"!fail", "!panic", "!ok"} {
		env.client.Deliver(direct(alice, text))
	}

	eventually(t, "three handled messages", func() bool { return len(env.rec.named("message_handled")) == 3 })

	failures := env.rec.failures(t)
	if failures["boom"] != 1 || failures["handler panicked: kaboom"] != 1 || len(failures) != 2 {
		t.Errorf("recorded failures %v, want boom and the panic once each", failures)
	}

	if got := len(env.logs.at(slog.LevelError)); got != 3 { // two failures and the panic's stack
		t.Errorf("logged %d ERROR records, want 3", got)
	}
}

// failures counts the errors recorded with message_handled by message, and
// checks that every event carries a duration.
func (r *recorder) failures(t *testing.T) map[string]int {
	t.Helper()

	failures := map[string]int{}

	for _, e := range r.named("message_handled") {
		if e.attrs["duration"].Kind() != slog.KindDuration {
			t.Errorf("message_handled lacks a duration: %v", e.attrs)
		}

		msg, failed := e.attrs["error"]
		if failed {
			failures[msg.String()]++
		}
	}

	return failures
}

// replyAll answers with a download, a reaction, a text and an image, and stops
// at the first failure.
func replyAll(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	data, err := chat.Download(ctx, msg.Media)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	steps := []func() error{
		func() error { return chat.React(ctx, "✅") },
		func() error { return chat.Send(ctx, "got "+string(data)) },
		func() error { return chat.SendImage(ctx, bot.Image{Data: data, MIME: "image/png", Caption: "echo"}) },
	}

	for _, step := range steps {
		err = step()
		if err != nil {
			return err
		}
	}

	return nil
}

func TestChatRepliesToTheMessageChat(t *testing.T) {
	t.Parallel()

	env := start(t, appFunc(replyAll), bot.Options{Groups: true})

	msg := env.client.Deliver(bot.Message{
		Sender: alice,
		Chat:   group,
		Group:  true,
		Text:   "!echo",
		Media:  &bot.Media{Kind: bot.MediaImage, Raw: []byte("pixels")},
	})

	eventually(t, "the handler to finish", func() bool { return len(env.rec.named("message_handled")) == 1 })

	if reactions := env.client.Reactions(); !slices.Equal(reactions, []fake.Reaction{{Chat: group, MessageID: msg.ID, Emoji: "✅"}}) {
		t.Errorf("reactions = %+v", reactions)
	}

	sent := env.client.Sent()
	if len(sent) != 2 || sent[0].To != group || sent[0].Text != "got pixels" ||
		sent[1].To != group || sent[1].Image == nil || sent[1].Image.Caption != "echo" {
		t.Errorf("sent = %+v", sent)
	}

	if _, failed := env.rec.named("message_handled")[0].attrs["error"]; failed {
		t.Error("handler failed")
	}
}

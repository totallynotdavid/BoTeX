package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/totallynotdavid/botkit/internal/ratelimit"
)

const (
	// DefaultMaxInFlight applies when Options.MaxInFlight is not positive.
	DefaultMaxInFlight = 10

	// shutdownGrace is how long Run waits for handlers to return once they
	// have been told to stop.
	shutdownGrace = 10 * time.Second

	rateLimitReaction = "⚠️"
	busyNotice        = "Too many concurrent requests. Please try again later."
)

var (
	// ErrRunTwice is returned by Run on a Bot that has already run.
	ErrRunTwice = errors.New("bot: Run called more than once")

	// ErrHandlerPanic wraps what an App's Handle panicked with.
	ErrHandlerPanic = errors.New("handler panicked")
)

// Reasons recorded with message_dropped.
const (
	dropFromMe       = "from_me"
	dropGroup        = "group"
	dropNotAllowed   = "not_allowed"
	dropRateLimited  = "rate_limited"
	dropBusy         = "busy"
	dropSessionEnded = "session_ended"
)

// Options configures a Bot. The zero value accepts direct messages from
// everyone, ignores the bot's own messages and imposes no rate limit.
type Options struct {
	// Groups accepts messages sent in groups.
	Groups bool
	// OwnMessages accepts messages the bot's own account sent, for example
	// typed on the paired phone.
	OwnMessages bool
	// AllowOnly limits the senders (Message.User) the bot answers. Empty
	// means everyone.
	AllowOnly []JID
	// Limiter limits requests per user. Nil disables the limit.
	Limiter *ratelimit.Limiter
	// Recorder receives the runtime's events. Nil logs them at debug level.
	Recorder Recorder
	// MaxInFlight caps concurrent Handle calls. A message that arrives at the
	// cap is dropped with a notice. DefaultMaxInFlight if not positive.
	MaxInFlight int
}

// Bot delivers the messages a Client receives to an App.
type Bot struct {
	client Client
	app    App
	log    *slog.Logger
	opts   Options
	rec    Recorder
	grace  time.Duration
	slots  chan struct{}

	started atomic.Bool

	// mu guards the fields below and orders handlers.Add against Wait: once
	// stopped is set no handler starts, so Wait sees a fixed set.
	mu       sync.Mutex
	stopped  bool
	ended    *SessionEndedError
	handlers sync.WaitGroup
	// ctx is the handlers' context. It is set by Run before the client can
	// deliver events.
	ctx    context.Context //nolint:containedctx // one context for the whole run, shared by every handler.
	cancel context.CancelCauseFunc
}

// New returns a Bot that gives the messages client receives to app. Run starts
// it.
func New(client Client, app App, log *slog.Logger, opts Options) *Bot {
	if log == nil {
		log = slog.Default()
	}

	rec := opts.Recorder
	if rec == nil {
		rec = logRecorder{log: log}
	}

	if opts.MaxInFlight <= 0 {
		opts.MaxInFlight = DefaultMaxInFlight
	}

	return &Bot{
		client: client,
		app:    app,
		log:    log,
		opts:   opts,
		rec:    rec,
		grace:  shutdownGrace,
		slots:  make(chan struct{}, opts.MaxInFlight),
	}
}

// Run connects and hands accepted messages to the app until ctx is done or
// the session ends. It returns ctx.Err() in the first case. In the second it
// returns the *SessionEndedError, after cancelling every in-flight handler
// with that error as the cause and giving them a grace period to return. A
// session end that happened before Run returned wins over a cancelled ctx. It
// returns Connect's error if the connection cannot start, which is a
// *SessionEndedError with Reason NotPaired when the client is not paired.
//
// A Bot runs once.
func (b *Bot) Run(ctx context.Context) error {
	if !b.started.CompareAndSwap(false, true) {
		return ErrRunTwice
	}

	// One context covers the dial, the run and every handler. It ends when the
	// caller cancels ctx or a session end cancels it with the error as cause.
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)

	b.ctx, b.cancel = runCtx, cancel

	connectErr := b.client.Connect(runCtx, b.onEvent)
	if end, ok := errors.AsType[*SessionEndedError](connectErr); ok {
		b.end(end)
	}

	if connectErr == nil {
		<-runCtx.Done()
	}

	b.stop()
	b.waitForHandlers()

	if connectErr == nil {
		b.client.Disconnect()
	}

	return b.result(ctx, connectErr)
}

// result is the one place Run's return value is decided, whichever way it
// ended:
//
//  1. A recorded session end is reported once and returned, even when ctx was
//     cancelled as well.
//  2. Otherwise a cancelled ctx returns its error.
//  3. Otherwise Connect's error is what ended Run.
func (b *Bot) result(ctx context.Context, connectErr error) error {
	end := b.sessionEnd()
	if end != nil {
		b.reportSessionEnd(ctx, end)

		return end
	}

	if ctx.Err() != nil {
		return ctx.Err() //nolint:wrapcheck // Run's contract is to return the context's error itself.
	}

	if connectErr != nil {
		return fmt.Errorf("connect: %w", connectErr)
	}

	return nil
}

func (b *Bot) onEvent(evt Event) {
	switch evt := evt.(type) {
	case MessageReceived:
		b.onMessage(evt.Message)
	case Connected:
		b.log.Info("whatsapp connected")
		b.rec.Record(b.ctx, "connected")
	case Disconnected:
		b.log.Warn("whatsapp disconnected, waiting for it to reconnect")
		b.rec.Record(b.ctx, "disconnected")
	case SessionEnded:
		b.end(&SessionEndedError{Reason: evt.Reason, Detail: evt.Detail})
	}
}

// onMessage runs on the connection's goroutine, so it only decides. Anything
// that talks to WhatsApp runs on a handler goroutine.
func (b *Bot) onMessage(msg Message) {
	b.rec.Record(b.ctx, "message_received", messageAttrs(msg)...)

	reason := b.filter(msg)
	if reason != "" {
		b.drop(msg, reason)

		return
	}

	chat := &Chat{client: b.client, msg: msg, ended: b.sessionEnd}

	if b.opts.Limiter != nil {
		result := b.opts.Limiter.Check(string(msg.User))
		if !result.Allowed {
			b.drop(msg, dropRateLimited)
			b.spawn(func(ctx context.Context) { b.rateLimited(ctx, chat, result) })

			return
		}
	}

	select {
	case b.slots <- struct{}{}:
	default:
		b.drop(msg, dropBusy)
		b.spawn(func(ctx context.Context) { b.busy(ctx, chat) })

		return
	}

	started := b.spawn(func(ctx context.Context) {
		defer func() { <-b.slots }()

		b.handle(ctx, chat, msg)
	})
	if !started {
		<-b.slots

		b.drop(msg, dropSessionEnded)
	}
}

// filter returns why msg is not for the app, or "" when it is.
func (b *Bot) filter(msg Message) string {
	switch {
	case msg.FromMe && !b.opts.OwnMessages:
		return dropFromMe
	case msg.Group && !b.opts.Groups:
		return dropGroup
	case len(b.opts.AllowOnly) > 0 && !slices.Contains(b.opts.AllowOnly, msg.User):
		return dropNotAllowed
	default:
		return ""
	}
}

func (b *Bot) drop(msg Message, reason string) {
	b.rec.Record(b.ctx, "message_dropped", append(messageAttrs(msg), slog.String("reason", reason))...)
}

// spawn runs work on its own goroutine unless the runtime has stopped, in which
// case it reports false.
func (b *Bot) spawn(work func(ctx context.Context)) bool {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()

		return false
	}

	b.handlers.Add(1)
	b.mu.Unlock()

	go func() {
		defer b.handlers.Done()

		work(b.ctx)
	}()

	return true
}

func (b *Bot) handle(ctx context.Context, chat *Chat, msg Message) {
	start := time.Now()
	err := b.call(ctx, chat, msg)

	attrs := append(messageAttrs(msg), slog.Duration("duration", time.Since(start)))
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))

		if !errors.As(err, new(*SessionEndedError)) {
			b.log.ErrorContext(ctx, "message handler failed", "id", msg.ID, "user", msg.User, "error", err)
		}
	}

	b.rec.Record(ctx, "message_handled", attrs...)
}

// call turns a panic in the app into an error, so one bad message does not
// take the bot down.
func (b *Bot) call(ctx context.Context, chat *Chat, msg Message) (err error) {
	defer func() {
		if r := recover(); r != nil {
			b.log.ErrorContext(ctx, "message handler panicked", "id", msg.ID, "panic", r, "stack", string(debug.Stack()))

			err = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
		}
	}()

	return b.app.Handle(ctx, msg, chat) //nolint:wrapcheck // the app's error is recorded as it returned it.
}

func (b *Bot) rateLimited(ctx context.Context, chat *Chat, result ratelimit.Result) {
	b.notify(ctx, chat.React(ctx, rateLimitReaction), "react to rate-limited message")

	if result.Notify {
		seconds := int(math.Ceil(result.ResetAfter.Seconds()))
		text := fmt.Sprintf("Too many requests. Please wait %d seconds.", seconds)

		b.notify(ctx, chat.Send(ctx, text), "send rate-limit notice")
	}
}

func (b *Bot) busy(ctx context.Context, chat *Chat) {
	b.notify(ctx, chat.React(ctx, rateLimitReaction), "react to message over the concurrency cap")
	b.notify(ctx, chat.Send(ctx, busyNotice), "send concurrency notice")
}

// notify logs a failed reply. A session that has ended is already reported.
func (b *Bot) notify(ctx context.Context, err error, what string) {
	if err != nil && !errors.As(err, new(*SessionEndedError)) {
		b.log.WarnContext(ctx, "failed to "+what, "error", err)
	}
}

// end stops the runtime for good. Only the first end counts.
func (b *Bot) end(end *SessionEndedError) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ended != nil {
		return
	}

	b.ended = end
	b.stopped = true

	b.cancel(end)
}

func (b *Bot) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.stopped = true
}

func (b *Bot) sessionEnd() *SessionEndedError {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.ended
}

func (b *Bot) waitForHandlers() {
	done := make(chan struct{})

	go func() {
		b.handlers.Wait()
		close(done)
	}()

	timer := time.NewTimer(b.grace)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		b.log.Warn("handlers still running after the grace period", "grace", b.grace)
	}
}

// reportSessionEnd tells the operator why the bot stopped and what to do.
func (b *Bot) reportSessionEnd(ctx context.Context, end *SessionEndedError) {
	b.rec.Record(ctx, "session_ended",
		slog.String("reason", end.Reason.String()),
		slog.String("detail", end.Detail),
	)

	b.log.ErrorContext(ctx, "whatsapp session ended",
		"reason", end.Reason.String(),
		"detail", end.Detail,
		"remedy", remedy(end.Reason),
	)
}

func remedy(reason SessionEndReason) string {
	switch reason {
	case NotPaired:
		return "no WhatsApp account is linked; run the pair command"
	case LoggedOut:
		return "the device was unlinked from the account; run the pair command to link it again"
	case Replaced:
		return "another process is using this session; stop it, then start the bot again"
	case Banned:
		return "WhatsApp banned the account for now; start the bot again after the ban expires"
	case Outdated:
		return "WhatsApp rejected this client version; update the bot"
	case Refused:
		return "WhatsApp refused the connection; check the detail, then start the bot again"
	default:
		return "start the bot again"
	}
}

func messageAttrs(m Message) []slog.Attr {
	return []slog.Attr{
		slog.String("id", m.ID),
		slog.String("chat", string(m.Chat)),
		slog.String("user", string(m.User)),
	}
}

package latex_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/png" // registers the decoder the tests use to read renders.
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/command"
	"github.com/totallynotdavid/botkit/internal/latex"
	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/typst"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const (
	member bot.JID = "51900000002@s.whatsapp.net"

	// wait bounds one message: a render, plus the runner's kill delay.
	wait = 30 * time.Second

	mitexWasmSHA256 = "f30000cbc2b18a6cdc72c3ce539d3ac443faa9109b4e0272cd274b37c6c1cff7"
)

// outcomes collects the result of every message the router finished.
type outcomes struct {
	mu   sync.Mutex
	errs []error
	done chan struct{}
}

func (o *outcomes) Record(_ context.Context, name string, attrs ...slog.Attr) {
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

	o.mu.Lock()
	o.errs = append(o.errs, err)
	o.mu.Unlock()

	o.done <- struct{}{}
}

// result is what one !latex message did.
type result struct {
	client  *fake.Client
	err     error
	elapsed time.Duration
}

func (r result) reactions() []string {
	reactions := r.client.Reactions()

	out := make([]string, 0, len(reactions))
	for _, reaction := range reactions {
		out = append(out, reaction.Emoji)
	}

	return out
}

// newService returns an auth service over a temporary database in which member
// may run latex.
func newService(t *testing.T) *auth.Service {
	t.Helper()

	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"), sqlite.WithoutSync())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	service, err := auth.New(t.Context(), database, auth.Rank{Name: "member", Level: 100, Commands: []string{"help", "latex"}})
	if err != nil {
		t.Fatal(err)
	}

	err = service.RegisterUser(t.Context(), string(member), "member", "test")
	if err != nil {
		t.Fatal(err)
	}

	return service
}

// render sends "!latex <code>" from a registered member through the router,
// the fake client and a real auth service, and waits for it to finish.
func render(t *testing.T, config latex.Config, code string) result {
	t.Helper()

	latexCommand, err := latex.New(config)
	if err != nil {
		t.Fatalf("latex.New: %v", err)
	}

	t.Cleanup(func() {
		closeErr := latexCommand.Close()
		if closeErr != nil {
			t.Errorf("close command: %v", closeErr)
		}
	})

	results := &outcomes{done: make(chan struct{}, 1)}
	client := fake.New()
	runtime := bot.New(client, command.NewRouter("!", newService(t), latexCommand), slog.New(slog.DiscardHandler), bot.Options{Recorder: results})

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

	err = client.WaitConnected(ctx)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	client.Deliver(bot.Message{Sender: member, Text: "!latex " + code})

	select {
	case <-results.done:
	case <-time.After(wait):
		t.Fatal("the router did not finish the message")
	}

	results.mu.Lock()
	defer results.mu.Unlock()

	return result{client: client, err: results.errs[0], elapsed: time.Since(start)}
}

// wantOneImage checks that the command sent exactly one PNG that decodes and
// reacted ✅.
func wantOneImage(t *testing.T, got result) {
	t.Helper()

	if got.err != nil {
		t.Fatalf("command failed: %v", got.err)
	}

	sent := got.client.Sent()
	if len(sent) != 1 || sent[0].Image == nil {
		t.Fatalf("sent %+v, want one image", sent)
	}

	if sent[0].Image.MIME != "image/png" {
		t.Errorf("MIME = %q, want image/png", sent[0].Image.MIME)
	}

	_, format, err := image.Decode(bytes.NewReader(sent[0].Image.Data))
	if err != nil || format != "png" {
		t.Errorf("image does not decode as a PNG: format %q, %v", format, err)
	}

	if reactions := got.reactions(); len(reactions) != 1 || reactions[0] != "✅" {
		t.Errorf("reactions = %v, want ✅", reactions)
	}
}

// wantNotice checks that the command sent one text containing want, no image,
// failed with cause (any error when cause is nil), and reacted ❌.
func wantNotice(t *testing.T, got result, cause error, want string) {
	t.Helper()

	if cause == nil {
		if got.err == nil {
			t.Error("command succeeded, want an error")
		}
	} else if !errors.Is(got.err, cause) {
		t.Errorf("command error = %v, want %v", got.err, cause)
	}

	sent := got.client.Sent()
	if len(sent) != 1 || sent[0].Image != nil || !strings.Contains(sent[0].Text, want) {
		t.Errorf("sent %+v, want one text containing %q", sent, want)
	}

	if reactions := got.reactions(); len(reactions) != 1 || reactions[0] != "❌" {
		t.Errorf("reactions = %v, want ❌", reactions)
	}
}

func TestFractionRenders(t *testing.T) {
	t.Parallel()

	wantOneImage(t, render(t, latex.DefaultConfig(), `\frac{a}{b}`))
}

func TestPhysicsMacrosRender(t *testing.T) {
	t.Parallel()

	wantOneImage(t, render(t, latex.DefaultConfig(), `\abs{x} + \norm{v} + \dv{f}{x} + \pdv{f}{x}`))
}

func TestRecursiveMacroHitsALimit(t *testing.T) {
	t.Parallel()

	got := render(t, latex.DefaultConfig(), `\newcommand{\x}{\x\x}\x`)

	wantNotice(t, got, typst.ErrLimit, "took too long or used too many resources")
	t.Logf("recursive \\newcommand answered in %v", got.elapsed)
}

// Two macros that call each other expand without growing memory, so only the
// deadline stops them.
func TestMutuallyRecursiveMacrosHitTheDeadline(t *testing.T) {
	t.Parallel()

	config := latex.DefaultConfig()
	config.Limits.Timeout = 2 * time.Second

	got := render(t, config, `\newcommand{\a}{\b}\newcommand{\b}{\a}\a`)

	wantNotice(t, got, typst.ErrLimit, "took too long or used too many resources")

	if got.elapsed > config.Limits.Timeout+typst.WaitDelay {
		t.Errorf("answered after %v, want within %v", got.elapsed, config.Limits.Timeout+typst.WaitDelay)
	}
}

func TestWideSpaceIsTooLarge(t *testing.T) {
	t.Parallel()

	got := render(t, latex.DefaultConfig(), `x \hspace{2000cm} y`)

	wantNotice(t, got, typst.ErrTooLarge, "too large")
}

func TestUnknownCommandQuotesMitex(t *testing.T) {
	t.Parallel()

	got := render(t, latex.DefaultConfig(), `\ce{H2O}`)

	wantNotice(t, got, nil, "unknown command: \\ce")

	renderErr, ok := errors.AsType[*typst.RenderError](got.err)
	if !ok || !strings.Contains(renderErr.Stderr, "unknown command: \\ce") {
		t.Errorf("command error = %v, want a *typst.RenderError quoting mitex", got.err)
	}

	if text := got.client.Sent()[0].Text; strings.Contains(text, "┌─") {
		t.Errorf("notice quotes typst's source trace: %q", text)
	}
}

func TestInputOverMaxLengthIsRefusedBeforeRendering(t *testing.T) {
	t.Parallel()

	config := latex.DefaultConfig()
	// A timeout no render can meet: only a refusal before typst runs ends in
	// ErrTooLong instead of ErrLimit.
	config.Limits.Timeout = time.Nanosecond

	got := render(t, config, strings.Repeat("x", config.MaxLength+1))

	wantNotice(t, got, latex.ErrTooLong, "longer than 1000 characters")
}

func TestMaxLengthCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()

	config := latex.DefaultConfig()
	config.MaxLength = 3

	wantOneImage(t, render(t, config, "ééé"))
	wantNotice(t, render(t, config, "éééé"), latex.ErrTooLong, "longer than 3 characters")
}

func TestEmptyInputGetsUsage(t *testing.T) {
	t.Parallel()

	wantNotice(t, render(t, latex.DefaultConfig(), ""), latex.ErrEmpty, "!latex")
}

func TestNewRejectsBadConfig(t *testing.T) {
	t.Parallel()

	config := latex.DefaultConfig()
	config.MaxLength = 0

	_, err := latex.New(config)
	if !errors.Is(err, latex.ErrInvalidConfig) {
		t.Errorf("New with no max length = %v, want ErrInvalidConfig", err)
	}

	config = latex.DefaultConfig()
	config.Limits.Timeout = 0

	_, err = latex.New(config)
	if !errors.Is(err, typst.ErrInvalidLimits) {
		t.Errorf("New with no timeout = %v, want ErrInvalidLimits", err)
	}
}

func TestVendoredMitexIsTheUpstreamRelease(t *testing.T) {
	t.Parallel()

	dir, cleanup, err := latex.ExtractMitex()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cleanupErr := cleanup()
		if cleanupErr != nil {
			t.Errorf("cleanup: %v", cleanupErr)
		}
	})

	pkg := filepath.Join(dir, "preview", "mitex", "0.2.7")

	wasm, err := os.ReadFile(filepath.Join(pkg, "mitex.wasm")) // #nosec G304 -- inside the directory the test just extracted.
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(wasm)
	if got := hex.EncodeToString(sum[:]); got != mitexWasmSHA256 {
		t.Errorf("mitex.wasm sha256 = %s, want %s", got, mitexWasmSHA256)
	}

	for _, name := range []string{"typst.toml", "lib.typ", "mitex.typ", "LICENSE", "specs/mod.typ", "specs/prelude.typ", "specs/latex/standard.typ"} {
		_, statErr := os.Stat(filepath.Join(pkg, filepath.FromSlash(name)))
		if statErr != nil {
			t.Errorf("extracted package lacks %s: %v", name, statErr)
		}
	}

	cleanupErr := cleanup()
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}

	_, statErr := os.Stat(dir)
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("cleanup left %s behind: %v", dir, statErr)
	}
}

package commands_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/totallynotdavid/botkit/pkg/commands"
	"github.com/totallynotdavid/botkit/pkg/config"
	"github.com/totallynotdavid/botkit/pkg/logger"
	"github.com/totallynotdavid/botkit/pkg/message"
)

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()

	factory, err := logger.NewFactory(logger.Config{Level: logger.ERROR, Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("failed to create logger factory: %v", err)
	}

	t.Cleanup(func() {
		closeErr := factory.Close()
		if closeErr != nil {
			t.Logf("failed to close logger factory: %v", closeErr)
		}
	})

	return factory.GetLogger("latex-test")
}

// requiredRenderTools mirrors the tools LaTeXCommand.initializeToolPaths
// looks for. The render-bound tests below need the real toolchain (TeX
// Live, ImageMagick, cwebp, prlimit from util-linux) because they assert on
// how pdflatex actually behaves under the configured limits; that can't be
// faked without testing something else entirely.
func requiredRenderTools() []string {
	return []string{"pdflatex", "convert", "cwebp", "prlimit"}
}

// newRenderTestCommand builds a real LaTeXCommand backed by the actual
// pdflatex/convert/cwebp/prlimit binaries resolved from PATH, the same way
// initializeToolPaths does in production. A missing tool skips with a named
// reason on a developer's machine, but is a hard test failure when
// BOTEX_REQUIRE_RENDER_TOOLS is set (as CI and the docker render-test image
// set it), so the resource bound this test proves can never pass silently
// by being skipped.
func newRenderTestCommand(t *testing.T) *commands.LaTeXCommand {
	t.Helper()

	return newRenderTestCommandWithSender(t, &fakeImageSender{})
}

func newRenderTestCommandWithSender(t *testing.T, sender *fakeImageSender) *commands.LaTeXCommand {
	t.Helper()

	requireRenderTools := os.Getenv("BOTEX_REQUIRE_RENDER_TOOLS") != ""

	for _, tool := range requiredRenderTools() {
		_, err := exec.LookPath(tool)
		if err != nil {
			reason := fmt.Sprintf("%s not found in PATH (TeX Live/ImageMagick/util-linux not installed on this host): %v", tool, err)
			if requireRenderTools {
				t.Fatalf("BOTEX_REQUIRE_RENDER_TOOLS is set: %s", reason)
			}

			t.Skipf("skipping: %s", reason)
		}
	}

	cfg := &config.Config{
		TempDir:      t.TempDir(),
		MaxImageSize: config.DefaultMaxImageSize,
	}

	return commands.NewTestLaTeXCommand(cfg, sender, newTestLogger(t))
}

// TestRenderLatex_LoopingMacroIsBoundedByResourceLimitsNotTimeout renders a
// self-referential macro that never terminates once expanded. A denylist
// cannot catch this (there is nothing to deny: \newcommand and the macro
// name are ordinary LaTeX), so the only thing that can stop it is the
// prlimit CPU bound applied to every external process a render runs. This
// must finish well inside the 45s wall-clock renderTimeout, killed instead
// by the much smaller maxRenderCPUSeconds budget, and the user must be told a
// limit was hit, not left with an error reaction that looks like a typo.
//
//nolint:paralleltest // measures real wall-clock time against a CPU rlimit; must not race against sibling subprocess-heavy tests for host CPU.
func TestRenderLatex_LoopingMacroIsBoundedByResourceLimitsNotTimeout(t *testing.T) {
	fake := &fakeImageSender{}
	latexCmd := newRenderTestCommandWithSender(t, fake)
	msg := &message.Message{Recipient: types.NewJID("1", "s.whatsapp.net")}

	const loopingMacro = `\newcommand{\loopforever}{\loopforever}\loopforever`

	start := time.Now()
	err := latexCmd.RenderAndSendLatex(context.Background(), loopingMacro, msg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected the looping macro render to fail, it did not (elapsed %s)", elapsed)
	}

	if !errors.Is(err, commands.ErrRenderLimitExceeded) {
		t.Fatalf("expected ErrRenderLimitExceeded, got: %v", err)
	}

	if fake.imageSent {
		t.Fatal("a render stopped by a limit must not send an image")
	}

	if !fake.textSent || fake.lastText == "" {
		t.Fatal("expected a user-facing text saying a limit was hit")
	}

	if elapsed >= latexCmd.RenderTimeout() {
		t.Fatalf("render was stopped by the %s wall-clock timeout, not by the resource bound (elapsed %s): %v",
			latexCmd.RenderTimeout(), elapsed, err)
	}

	t.Logf("looping macro render stopped after %s via the %ds CPU bound (renderTimeout is %s): %v",
		elapsed, latexCmd.MaxRenderCPUSeconds(), latexCmd.RenderTimeout(), err)
}

// TestRenderAndSendLatex_SyntaxErrorIsNotALimit is the counterpart: an
// ordinary LaTeX error is pdflatex exiting on its own, not a resource bound
// stopping it, so it must neither be classified as one nor produce the
// limit text (the caller reacts to the error instead).
//
//nolint:paralleltest // shares the render toolchain's CPU budget with the timing-sensitive tests.
func TestRenderAndSendLatex_SyntaxErrorIsNotALimit(t *testing.T) {
	fake := &fakeImageSender{}
	latexCmd := newRenderTestCommandWithSender(t, fake)
	msg := &message.Message{Recipient: types.NewJID("1", "s.whatsapp.net")}

	err := latexCmd.RenderAndSendLatex(context.Background(), `\undefinedmacro{x}`, msg)
	if err == nil {
		t.Fatal("expected a LaTeX error to fail the render")
	}

	if errors.Is(err, commands.ErrRenderLimitExceeded) {
		t.Fatalf("a syntax error must not be classified as a limit stop: %v", err)
	}

	if fake.textSent || fake.imageSent {
		t.Fatalf("nothing should be sent for a syntax error (text=%v image=%v)", fake.textSent, fake.imageSent)
	}
}

// TestExecuteSecuredCommand_DeadlineIsALimitButPlainFailureIsNot classifies
// from how the process ended (context deadline, exit status), never from
// what it printed.
//
//nolint:paralleltest // spawns real subprocesses under prlimit; keeps timing predictable.
func TestExecuteSecuredCommand_DeadlineIsALimitButPlainFailureIsNot(t *testing.T) {
	latexCmd := newRenderTestCommand(t)

	shPath, lookErr := exec.LookPath("sh")
	if lookErr != nil {
		t.Skipf("skipping: sh not found in PATH: %v", lookErr)
	}

	deadlineCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, deadlineErr := latexCmd.ExecuteSecuredCommandCapture(deadlineCtx, "", "test-deadline", shPath, "-c", "sleep 30")
	if !errors.Is(deadlineErr, commands.ErrRenderLimitExceeded) {
		t.Fatalf("a process stopped by the context deadline must be ErrRenderLimitExceeded, got: %v", deadlineErr)
	}

	// The text mentions "limit" and "killed"; only the exit status counts.
	_, _, failErr := latexCmd.ExecuteSecuredCommandCapture(
		context.Background(), "", "test-plain-failure", shPath, "-c", "echo 'resource limit exceeded, killed'; exit 1")
	if failErr == nil {
		t.Fatal("expected a non-zero exit to be an error")
	}

	if errors.Is(failErr, commands.ErrRenderLimitExceeded) {
		t.Fatalf("an ordinary non-zero exit must not be a limit stop: %v", failErr)
	}
}

// TestRenderLatex_CsnameBuiltInputBypassesDenylistButIsBlocked builds \input
// out of \csname so the literal string "\input" never appears in the
// source, defeating validateLatexContent's substring check. It still fails,
// almost immediately, because pdflatex is run with openin_any=p: paranoid
// kpathsea file access refuses to open a file outside the render's own
// working directory regardless of how the path reached \openin.
//
//nolint:paralleltest // measures real wall-clock time against a CPU rlimit; must not race against sibling subprocess-heavy tests for host CPU.
func TestRenderLatex_CsnameBuiltInputBypassesDenylistButIsBlocked(t *testing.T) {
	latexCmd := newRenderTestCommand(t)

	const bypass = `\csname input\endcsname{/etc/passwd}` // #nosec G101 -- a file path used as a render-bound test fixture, not a credential.

	validateErr := latexCmd.ValidateLatexContent(bypass)
	if validateErr != nil {
		t.Fatalf("expected the denylist to miss the \\csname-built \\input, got: %v", validateErr)
	}

	start := time.Now()
	_, err := latexCmd.RenderLatex(context.Background(), bypass)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected the render to fail, it did not (elapsed %s)", elapsed)
	}

	if elapsed >= latexCmd.RenderTimeout() {
		t.Fatalf("render took %s, expected openin_any=p to reject the file almost immediately", elapsed)
	}

	t.Logf("csname-built \\input was rejected by openin_any=p after %s: %v", elapsed, err)
}

// TestRenderLatex_ValidEquationStillRenders is the control: the resource
// bounds and openin_any/openout_any restriction must not break an ordinary
// render.
//
//nolint:paralleltest // shares the render toolchain's CPU budget with the timing-sensitive tests above.
func TestRenderLatex_ValidEquationStillRenders(t *testing.T) {
	latexCmd := newRenderTestCommand(t)

	webpImage, err := latexCmd.RenderLatex(context.Background(), `x = \frac{-b \pm \sqrt{b^2-4ac}}{2a}`)
	if err != nil {
		t.Fatalf("expected a valid equation to render, got: %v", err)
	}

	if len(webpImage) == 0 {
		t.Fatal("expected a non-empty rendered image")
	}

	t.Logf("valid equation rendered to %d bytes", len(webpImage))
}

func TestValidateLatexContent(t *testing.T) {
	t.Parallel()

	latexCmd := commands.NewTestLaTeXCommand(&config.Config{}, &fakeImageSender{}, newTestLogger(t))

	cases := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{"input is blocked", `\input{/etc/passwd}`, true},
		{"include is blocked", `\include{foo}`, true},
		{"write18 is blocked", `\write18{ls}`, true},
		{"def is no longer denylisted", `\def\x{1}`, false},
		{"let is no longer denylisted", `\let\x=\y`, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := latexCmd.ValidateLatexContent(testCase.code)
			if testCase.wantErr && err == nil {
				t.Fatalf("expected an error for %q", testCase.code)
			}

			if !testCase.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", testCase.code, err)
			}
		})
	}
}

type fakeImageSender struct {
	imageSent bool
	textSent  bool
	lastText  string
}

func (f *fakeImageSender) SendImage(_ context.Context, _ types.JID, _ []byte, _ string) error {
	f.imageSent = true

	return nil
}

func (f *fakeImageSender) SendText(_ context.Context, _ types.JID, text string) error {
	f.textSent = true
	f.lastText = text

	return nil
}

func TestDeliverImage_OverCapIsNotSent(t *testing.T) {
	t.Parallel()

	fake := &fakeImageSender{}
	latexCmd := commands.NewTestLaTeXCommand(&config.Config{MaxImageSize: 10}, fake, newTestLogger(t))
	msg := &message.Message{Recipient: types.NewJID("1", "s.whatsapp.net")}

	err := latexCmd.DeliverImage(context.Background(), make([]byte, 11), msg)

	if !errors.Is(err, commands.ErrImageTooLarge) {
		t.Fatalf("expected ErrImageTooLarge, got %v", err)
	}

	if fake.imageSent {
		t.Fatal("oversized image must not be sent")
	}

	if !fake.textSent || fake.lastText == "" {
		t.Fatal("expected a clear user-facing message when the image is refused")
	}
}

func TestTailWriter_UnderCapKeepsEverything(t *testing.T) {
	t.Parallel()

	writer := commands.NewTailWriterForTest(16)

	_, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf, truncated := writer.Bytes()
	if truncated {
		t.Fatal("expected no truncation when written bytes are under the cap")
	}

	if string(buf) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(buf))
	}
}

func TestTailWriter_ExactlyAtCapKeepsEverything(t *testing.T) {
	t.Parallel()

	writer := commands.NewTailWriterForTest(5)

	_, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf, truncated := writer.Bytes()
	if truncated {
		t.Fatal("expected no truncation when written bytes exactly fill the cap")
	}

	if string(buf) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(buf))
	}
}

func TestTailWriter_OverCapKeepsOnlyTheTail(t *testing.T) {
	t.Parallel()

	writer := commands.NewTailWriterForTest(5)

	for _, chunk := range []string{"one-", "two-", "three"} {
		_, err := writer.Write([]byte(chunk))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	buf, truncated := writer.Bytes()
	if !truncated {
		t.Fatal("expected truncation: wrote 13 bytes into a 5 byte cap")
	}

	if string(buf) != "three" {
		t.Fatalf("expected the tail to preserve only the final bytes written (%q), got %q", "three", string(buf))
	}
}

func TestTailWriter_OverCapInASingleWriteKeepsTheTail(t *testing.T) {
	t.Parallel()

	writer := commands.NewTailWriterForTest(8)

	payload := append(bytes.Repeat([]byte("x"), 1000), []byte("MARKER!!")...)

	_, err := writer.Write(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf, truncated := writer.Bytes()
	if !truncated {
		t.Fatal("expected truncation")
	}

	if string(buf) != "MARKER!!" {
		t.Fatalf("expected the tail to be %q, got %q", "MARKER!!", string(buf))
	}
}

// TestExecuteSecuredCommand_OutputIsBoundedNotBuffered proves the fix for
// the gap CombinedOutput left: RLIMIT_FSIZE bounds regular files, not a
// pipe, so a process that only prints can otherwise grow the bot's own
// memory without limit. `yes | head -c 50000000` writes 50MB through a
// pipe (no regular file involved, so the prlimit fsize bound does not
// apply), which is what makes this a real test of the capture path rather
// than of prlimit.
//
//nolint:paralleltest // shares the render toolchain's CPU budget with the other render tests in this file.
func TestExecuteSecuredCommand_OutputIsBoundedNotBuffered(t *testing.T) {
	latexCmd := newRenderTestCommand(t)

	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("sh not found in PATH: %v", err)
	}

	const marker = "FINAL-MARKER-XYZ"

	script := "yes AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA | head -c 50000000; echo " + marker

	output, truncated, err := latexCmd.ExecuteSecuredCommandCapture(context.Background(), "", "test-overflow", shPath, "-c", script)
	if err != nil {
		t.Fatalf("unexpected error running the overflow command: %v", err)
	}

	if !truncated {
		t.Fatal("expected the captured output to report truncation: 50MB was written, far more than the cap")
	}

	if len(output) > commands.MaxLoggedOutputBytes() {
		t.Fatalf("captured output is %d bytes, exceeds the %d byte cap", len(output), commands.MaxLoggedOutputBytes())
	}

	if !strings.Contains(string(output), marker) {
		t.Fatalf("expected the captured tail to hold the final line %q, got %q", marker, truncateForLog(output))
	}
}

func truncateForLog(output []byte) string {
	const maxLogPreview = 200
	if len(output) > maxLogPreview {
		return string(output[len(output)-maxLogPreview:])
	}

	return string(output)
}

func TestDeliverImage_UnderCapIsSent(t *testing.T) {
	t.Parallel()

	fake := &fakeImageSender{}
	latexCmd := commands.NewTestLaTeXCommand(&config.Config{MaxImageSize: 10}, fake, newTestLogger(t))
	msg := &message.Message{Recipient: types.NewJID("1", "s.whatsapp.net")}

	err := latexCmd.DeliverImage(context.Background(), []byte("ok"), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fake.imageSent {
		t.Fatal("expected the image to be sent")
	}

	if fake.textSent {
		t.Fatal("no size-limit message should be sent when the image is within the cap")
	}
}

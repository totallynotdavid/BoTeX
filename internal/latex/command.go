// Package latex is the "latex" chat command: it renders LaTeX math to a PNG
// with typst and the vendored mitex package, under the resource limits of
// internal/typst.
package latex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/command"
	"github.com/totallynotdavid/botkit/internal/typst"
)

const (
	defaultMaxLength = 1000
	defaultTypst     = "typst"
	// maxQuoteRunes bounds how much of mitex's message a reply quotes.
	maxQuoteRunes = 400

	inputFile = "input.tex"

	limitNotice = "That equation took too long or used too many resources to render. Try a shorter or simpler expression."
	mitexTrap   = "wasm `unreachable` instruction executed"

	document = `#import "@preview/mitex:0.2.7": mitex
#set page(width: auto, height: auto, margin: 8pt, fill: white)
#set text(size: 14pt)
#mitex(read("` + inputFile + `"))
`

	// preamble restores the few macros of the physics package that mitex
	// lacks. \qty cannot be defined this way and is not supported.
	preamble = `\newcommand{\abs}[1]{\left|#1\right|}
\newcommand{\norm}[1]{\left\|#1\right\|}
\newcommand{\dv}[2]{\frac{d #1}{d #2}}
\newcommand{\pdv}[2]{\frac{\partial #1}{\partial #2}}
`
)

var (
	// ErrEmpty is returned by Run when there is nothing to render.
	ErrEmpty = errors.New("no LaTeX given")
	// ErrTooLong is returned by Run for input over Config.MaxLength.
	ErrTooLong = errors.New("LaTeX is too long")
	// ErrInvalidConfig is returned by New for a Config that cannot work.
	ErrInvalidConfig = errors.New("invalid latex config")
)

// Config bounds what the command renders.
type Config struct {
	// Typst is the typst executable: a path, or a name looked up on PATH.
	Typst string
	// MaxLength is the most characters of LaTeX the command accepts.
	MaxLength int
	// Limits bound each render, including the byte cap of the image.
	Limits typst.Limits
}

// DefaultConfig returns the limits a chat command runs under.
func DefaultConfig() Config {
	return Config{Typst: defaultTypst, MaxLength: defaultMaxLength, Limits: typst.DefaultLimits()}
}

// Command renders "!latex <code>" to an image.
type Command struct {
	maxLength int
	runner    *typst.Runner
	cleanup   func() error
}

var _ command.Command = (*Command)(nil)

// New extracts the vendored mitex and returns a Command that renders with
// config.Typst. Close it to remove the extracted files.
func New(config Config) (*Command, error) {
	if config.MaxLength <= 0 {
		return nil, fmt.Errorf("%w: max length %d", ErrInvalidConfig, config.MaxLength)
	}

	packages, cleanup, err := extractMitex()
	if err != nil {
		return nil, err
	}

	runner, err := typst.New(config.Typst, packages, config.Limits)
	if err != nil {
		return nil, errors.Join(err, cleanup())
	}

	return &Command{maxLength: config.MaxLength, runner: runner, cleanup: cleanup}, nil
}

// Close removes the files New extracted.
func (c *Command) Close() error {
	return c.cleanup()
}

func (c *Command) Name() string { return "latex" }

func (c *Command) Info() command.Info {
	return command.Info{
		Description: "Render LaTeX equations into images",
		Usage:       "!latex <equation>",
		Examples: []string{
			`!latex x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`,
			`!latex \int_{a}^{b} f(x)\,dx = F(b) - F(a)`,
		},
	}
}

// Run renders args and sends the image. Every failure is answered with a
// short text and returned, so the router reacts ❌.
func (c *Command) Run(ctx context.Context, _ bot.Message, args string, chat *bot.Chat) error {
	if args == "" {
		return refuse(ctx, chat, ErrEmpty, "Give me some LaTeX to render, for example `!latex \\frac{a}{b}`.")
	}

	if utf8.RuneCountInString(args) > c.maxLength {
		return refuse(ctx, chat, ErrTooLong, fmt.Sprintf("That equation is longer than %d characters. Try a shorter one.", c.maxLength))
	}

	png, err := c.runner.Render(ctx, document, map[string]string{inputFile: preamble + args})
	if err != nil {
		return explain(ctx, chat, err)
	}

	err = chat.SendImage(ctx, bot.Image{Data: png, MIME: "image/png"})
	if err != nil {
		return fmt.Errorf("send equation: %w", err)
	}

	return nil
}

// explain answers a failed render with the reason the sender can act on and
// returns the failure.
func explain(ctx context.Context, chat *bot.Chat, err error) error {
	var renderErr *typst.RenderError

	switch {
	case errors.Is(err, typst.ErrLimit):
		return refuse(ctx, chat, err, limitNotice)
	case errors.Is(err, typst.ErrTooLarge):
		return refuse(ctx, chat, err, "The rendered equation is too large to send. Try a shorter or simpler expression.")
	case errors.As(err, &renderErr) && strings.Contains(renderErr.Stderr, mitexTrap):
		// A macro that expands without end fills mitex's WASM memory until
		// the allocation fails and it traps. The trap is the data limit
		// at work, so it is reported as one.
		return refuse(ctx, chat, errors.Join(typst.ErrLimit, err), limitNotice)
	case errors.As(err, &renderErr):
		return refuse(ctx, chat, err, "That LaTeX could not be rendered:\n```"+quote(renderErr.Stderr)+"```")
	default:
		return fmt.Errorf("render equation: %w", err)
	}
}

// refuse tells the sender why nothing was rendered and returns cause, joined
// with any failure to send the notice.
func refuse(ctx context.Context, chat *bot.Chat, cause error, notice string) error {
	return errors.Join(fmt.Errorf("latex: %w", cause), chat.Send(ctx, notice))
}

// quote keeps the first line of typst's message, which states the cause; the
// rest is a trace of typst's own source. It is bounded to fit a chat bubble.
func quote(stderr string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	first = strings.TrimSpace(strings.Replace(first, "plugin errored with: ", "", 1))

	if utf8.RuneCountInString(first) <= maxQuoteRunes {
		return first
	}

	return string([]rune(first)[:maxQuoteRunes]) + "…"
}

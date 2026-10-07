// Package envfile writes .env.example. The defaults in it are not typed here:
// they are what each bot's own code reads when nothing is set, so the file
// cannot disagree with the bots. Only the explanation of each setting lives in
// this package.
package envfile

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/latex"
)

const (
	width = 79
	// minDashes keeps a heading's rule visible when its title is long.
	minDashes = 3
)

var (
	// ErrUndocumented is wrapped by Example's error for a setting a bot reads
	// and this package does not explain.
	ErrUndocumented = errors.New("setting has no explanation in package envfile")
	// ErrUnread is wrapped by Example's error for an explanation of a setting
	// no bot reads.
	ErrUnread = errors.New("explained setting is read by no bot")
)

const preamble = `Copy to .env (mise and systemd's EnvironmentFile= read it). Every line is
optional; a blank value uses the default, which a note gives when the bots
differ.
Generated from the settings the bots read. To change it, change the code and
run: mise run env:example`

// Bot is a bot and every setting it reads, with the default it uses for each.
type Bot struct {
	Name    string
	Entries []config.Entry
}

// Bots returns the bots as their binaries configure them. Each binary's own
// command must read the same settings: the tests of cmd/latex and cmd/flow
// compare them.
func Bots() []Bot {
	latexBot := cli.Command{
		Name: "latex",
		Configure: func(env *config.Env) cli.Build {
			latex.ConfigFromEnv(env)

			return nil
		},
	}

	flowBot := cli.Command{
		Name:      "flow",
		RateLimit: flow.DefaultRateLimit(),
		Configure: func(env *config.Env) cli.Build {
			flow.ConfigFromEnv(env)

			return nil
		},
	}

	return []Bot{
		{Name: latexBot.Name, Entries: cli.Describe(latexBot)},
		{Name: flowBot.Name, Entries: cli.Describe(flowBot)},
	}
}

type doc struct{ key, text string }

// docs explains each setting, in the order the file lists them. A doc says
// what a setting does and never what its default is.
//
//nolint:gochecknoglobals // a read-only table.
var docs = []doc{
	{config.KeyStore, "SQLite file holding the WhatsApp session, users and ranks."},
	{config.KeyLogLevel, "debug, info, warn or error."},
	{config.KeyOwners, "Comma-separated JIDs registered as owners on every start, for example " +
		"51999999999@s.whatsapp.net. Without one, nobody can run a command until a user is added to the " +
		"database by hand."},
	{config.KeyRateLimitRequests, "Requests each user may send per period. Over the limit the bot reacts " +
		"with a warning and sends one notice per cooldown."},
	{config.KeyRateLimitPeriod, "How long a request counts against the limit, as a Go duration."},
	{config.KeyRateLimitCooldown, "Least time between two notices to a user who stays over the limit, " +
		"as a Go duration."},
	{config.KeyMaxInFlight, `Messages handled at once. Beyond it the bot answers "too many requests".`},
	{config.KeyOwnMessages, "Answer messages sent from the bot's own WhatsApp account."},
	{config.KeyAllowOnly, "Comma-separated JIDs the bot answers. Messages from anyone else are ignored. " +
		"Empty answers everyone. Useful to test a bot on a number others also message."},

	{latex.KeyMaxLength, "Most characters of LaTeX one message may hold."},
	{latex.KeyMaxImageSize, "Largest PNG the bot sends, in bytes."},
	{latex.KeyTimeout, "Longest a render may run, as a Go duration."},
	{latex.KeyDataLimit, "Data memory the typst process may use, in bytes."},
	{latex.KeyFileLimit, "Largest file the typst process may write, in bytes."},

	{flow.KeyFile, "JSON file with the conversation. Empty runs the example flow built into the binary. " +
		"A file that cannot be read or is not a valid flow stops the bot."},
	{flow.KeyVoucherDir, "Directory for the payment vouchers users send. Created on the first one."},
	{flow.KeyTypingDelay, "How long the bot waits before each reply, as a Go duration. 0s replies at once."},
}

// Example renders .env.example for bots. It fails when a bot reads a setting
// that has no explanation here, or an explanation names a setting no bot
// reads, so a setting cannot be added or dropped without the file following.
func Example(bots []Bot) (string, error) {
	err := checkDocs(bots)
	if err != nil {
		return "", err
	}

	var out strings.Builder

	out.WriteString(comment(preamble))

	for _, section := range sections(bots) {
		out.WriteString("\n" + heading(section.title) + "\n")

		for _, key := range section.keys {
			out.WriteString("\n" + comment(docOf(key)))

			value, note := render(bots, key)
			if note != "" {
				out.WriteString(comment(note))
			}

			out.WriteString(key + "=" + value + "\n")
		}
	}

	return out.String(), nil
}

func checkDocs(bots []Bot) error {
	var problems []error

	read := map[string]bool{}

	for _, bot := range bots {
		for _, entry := range bot.Entries {
			read[entry.Key] = true

			if !slices.ContainsFunc(docs, func(d doc) bool { return d.key == entry.Key }) {
				problems = append(problems, fmt.Errorf("%w: %s reads %s", ErrUndocumented, bot.Name, entry.Key))
			}
		}
	}

	for _, doc := range docs {
		if !read[doc.key] {
			problems = append(problems, fmt.Errorf("%w: %s", ErrUnread, doc.key))
		}
	}

	return errors.Join(problems...)
}

type section struct {
	title string
	keys  []string
}

// sections puts the settings every bot reads first and then each bot's own,
// each in the order of docs.
func sections(bots []Bot) []section {
	result := make([]section, 1, 1+len(bots))
	result[0].title = "Shared by every bot"

	for _, bot := range bots {
		result = append(result, section{title: bot.Name + " bot"})
	}

	for _, doc := range docs {
		readers := 0
		owner := 0

		for i, bot := range bots {
			if reads(bot, doc.key) {
				readers++
				owner = i + 1
			}
		}

		index := 0
		if readers < len(bots) {
			index = owner
		}

		result[index].keys = append(result[index].keys, doc.key)
	}

	return result
}

func reads(bot Bot, key string) bool {
	return slices.ContainsFunc(bot.Entries, func(entry config.Entry) bool { return entry.Key == key })
}

// render returns the value to write for key. When the bots that read it have
// different defaults, the value is blank and the note lists them.
func render(bots []Bot, key string) (value, note string) {
	var defaults []string

	var parts []string

	for _, bot := range bots {
		i := slices.IndexFunc(bot.Entries, func(entry config.Entry) bool { return entry.Key == key })
		if i < 0 {
			continue
		}

		def := bot.Entries[i].Default
		if !slices.Contains(defaults, def) {
			defaults = append(defaults, def)
		}

		parts = append(parts, fmt.Sprintf("%s %s", bot.Name, quoteEmpty(def)))
	}

	if len(defaults) == 1 {
		return defaults[0], ""
	}

	return "", "Defaults: " + strings.Join(parts, ", ") + "."
}

func quoteEmpty(def string) string {
	if def == "" {
		return "none"
	}

	return def
}

func docOf(key string) string {
	for _, doc := range docs {
		if doc.key == key {
			return doc.text
		}
	}

	return ""
}

func heading(title string) string {
	line := "# " + title + " "

	return line + strings.Repeat("-", max(width-len(line), minDashes))
}

// comment wraps text into "# " lines of at most width columns. Paragraphs
// stay apart at a blank line.
func comment(text string) string {
	var out strings.Builder

	for i, paragraph := range strings.Split(text, "\n\n") {
		if i > 0 {
			out.WriteString("#\n")
		}

		line := ""

		for word := range strings.FieldsSeq(paragraph) {
			if line != "" && len("# "+line+" "+word) > width {
				out.WriteString("# " + line + "\n")

				line = ""
			}

			if line != "" {
				line += " "
			}

			line += word
		}

		out.WriteString("# " + line + "\n")
	}

	return out.String()
}

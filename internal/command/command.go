// Package command routes chat messages of the form "!name args" to commands.
package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
)

const (
	reactionOK     = "✅"
	reactionFailed = "❌"
	reactionDenied = "🚫"
)

// Info describes a command for the help listing.
type Info struct {
	Description string
	Usage       string
	Examples    []string
}

// Command is one "!name" a user can run.
type Command interface {
	Name() string
	Info() Info
	// Run handles the command. args is the text after the name, trimmed but
	// otherwise as the user typed it. A returned error makes the router react
	// with ❌ and report the error to the runtime.
	Run(ctx context.Context, m bot.Message, args string, c *bot.Chat) error
}

// Permissions says whether a user may run a command. *auth.Service satisfies
// it.
type Permissions interface {
	CheckPermission(ctx context.Context, user, command string) (bool, error)
}

// Router is a bot.App that runs the command a message names, after checking
// that its sender may. It reacts ✅ when the command succeeds, ❌ when it fails
// or does not exist, and 🚫 when the sender is not allowed.
type Router struct {
	prefix   string
	perms    Permissions
	commands map[string]Command
}

var _ bot.App = (*Router)(nil)

// NewRouter routes messages that start with prefix to cmds and to a help
// command it adds. It panics if two commands share a name, which is a
// programming error.
func NewRouter(prefix string, perms Permissions, cmds ...Command) *Router {
	router := &Router{prefix: prefix, perms: perms, commands: make(map[string]Command, len(cmds)+1)}

	for _, cmd := range cmds {
		router.add(cmd)
	}

	router.add(&help{prefix: prefix, listed: cmds, all: router.commands})

	return router
}

// Handle implements bot.App. A message without the prefix is not for the
// router and is ignored.
func (r *Router) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	name, args, ok := r.parse(msg.Text)
	if !ok {
		return nil
	}

	if auth.ValidateCommand(name) != nil {
		return r.unknown(ctx, chat, name)
	}

	allowed, err := r.perms.CheckPermission(ctx, string(msg.User), name)
	if err != nil {
		return fmt.Errorf("check permission for %q: %w", name, err)
	}

	if !allowed {
		return r.reply(ctx, chat, reactionDenied,
			fmt.Sprintf("You do not have permission to use the `%s%s` command.", r.prefix, name))
	}

	cmd, found := r.commands[name]
	if !found {
		return r.unknown(ctx, chat, name)
	}

	err = cmd.Run(ctx, msg, args, chat)
	if err != nil {
		return errors.Join(fmt.Errorf("command %q: %w", name, err), chat.React(ctx, reactionFailed))
	}

	err = chat.React(ctx, reactionOK)
	if err != nil {
		return fmt.Errorf("command %q: %w", name, err)
	}

	return nil
}

func (r *Router) add(cmd Command) {
	if _, taken := r.commands[cmd.Name()]; taken {
		panic(fmt.Sprintf("command: %q registered twice", cmd.Name()))
	}

	r.commands[cmd.Name()] = cmd
}

// parse splits "!name args" into its name and arguments.
func (r *Router) parse(text string) (name, args string, ok bool) {
	body, hasPrefix := strings.CutPrefix(strings.TrimSpace(text), r.prefix)
	body = strings.TrimSpace(body)

	if !hasPrefix || body == "" {
		return "", "", false
	}

	end := strings.IndexFunc(body, unicode.IsSpace)
	if end < 0 {
		return body, "", true
	}

	return body[:end], strings.TrimSpace(body[end:]), true
}

func (r *Router) unknown(ctx context.Context, chat *bot.Chat, name string) error {
	return r.reply(ctx, chat, reactionFailed,
		fmt.Sprintf("Command %#q not found. Use `%shelp` to see available commands.", r.prefix+name, r.prefix))
}

func (r *Router) reply(ctx context.Context, chat *bot.Chat, reaction, text string) error {
	return errors.Join(chat.React(ctx, reaction), chat.Send(ctx, text))
}

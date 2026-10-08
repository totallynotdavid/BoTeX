package latex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/command"
)

var errInvalidApp = errors.New("latex app: router, permissions and database are required")

const welcome = "Hi! I’m *Tinta*, your friendly math sidekick. ✨\n\n" +
	"Send `!latex <equation>` and I’ll turn it into a crisp PNG.\n" +
	"Try `!latex \\frac{a}{b}` or `!help` for the full menu.\n\n" +
	"I’ll keep my replies short and react ✅ when a command works."

const welcomeBack = "Welcome back! ✨ Use `!latex <equation>` to render math, or `!help` to see the menu."

const welcomeSchema = `
CREATE TABLE IF NOT EXISTS latex_user_state (
    user_id TEXT PRIMARY KEY,
    first_seen DATETIME NOT NULL
)
`

// App handles greetings before routing other messages to the command app. It
// uses the command router's permission service and persists first visits in
// SQLite.
type App struct {
	router      bot.App
	permissions command.Permissions
	database    *sql.DB
}

var _ bot.App = (*App)(nil)

// NewApp creates an App backed by database. The database also holds auth and
// WhatsApp session state, so a restart preserves each user's first visit.
func NewApp(ctx context.Context, router bot.App, permissions command.Permissions, database *sql.DB) (*App, error) {
	if router == nil || permissions == nil || database == nil {
		return nil, errInvalidApp
	}

	_, err := database.ExecContext(ctx, welcomeSchema)
	if err != nil {
		return nil, fmt.Errorf("create latex user state: %w", err)
	}

	return &App{router: router, permissions: permissions, database: database}, nil
}

// Handle authorizes a greeting before it can produce any reply. This keeps
// group registration and user registration identical to command handling.
func (a *App) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	text := strings.ToLower(strings.TrimSpace(msg.Text))
	if isGreetingMessage(text) {
		return a.welcome(ctx, msg, chat)
	}

	err := a.router.Handle(ctx, msg, chat)
	if err != nil {
		return fmt.Errorf("route message: %w", err)
	}

	return nil
}

func (a *App) welcome(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	allowed, err := a.authorized(ctx, msg)
	if err != nil {
		return fmt.Errorf("authorize welcome: %w", err)
	}

	if !allowed {
		return nil
	}

	first, err := a.firstVisit(ctx, msg.User)
	if err != nil {
		return err
	}

	message := welcome
	if !first {
		message = welcomeBack
	}

	err = chat.React(ctx, "👋")
	if err != nil {
		return fmt.Errorf("welcome reaction: %w", err)
	}

	err = chat.Send(ctx, message)
	if err != nil {
		return fmt.Errorf("welcome: %w", err)
	}

	return nil
}

func (a *App) authorized(ctx context.Context, msg bot.Message) (bool, error) {
	group := ""
	if msg.Group {
		group = string(msg.Chat)
	}

	decision, err := a.permissions.Authorize(ctx, string(msg.User), group, "help")
	if err != nil {
		return false, fmt.Errorf("authorize user %s in group %s: %w", msg.User, group, err)
	}

	return decision == auth.Allowed, nil
}

func isGreetingMessage(text string) bool {
	return text == "!start" || text == "!menu" || isGreeting(text)
}

// firstVisit records the user's first visit and reports whether this call made it.
// The primary key and DO NOTHING let at most one of several concurrent
// greetings see true. The state lives only in the database, so every process
// sharing it agrees.
func (a *App) firstVisit(ctx context.Context, user bot.JID) (bool, error) {
	result, err := a.database.ExecContext(ctx,
		`INSERT INTO latex_user_state (user_id, first_seen) VALUES (?, CURRENT_TIMESTAMP) ON CONFLICT(user_id) DO NOTHING`, user)
	if err != nil {
		return false, fmt.Errorf("save first visit of %s: %w", user, err)
	}

	created, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check first visit of %s: %w", user, err)
	}

	return created == 1, nil
}

func isGreeting(text string) bool {
	switch text {
	case "hi", "hello", "hey", "hola", "buenas", "buenos días", "buenas tardes", "buenas noches":
		return true
	default:
		return false
	}
}

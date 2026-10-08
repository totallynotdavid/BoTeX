package cli

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/whatsapp"
)

// OpenClient returns the WhatsApp connection over database.
type OpenClient func(ctx context.Context, database *sql.DB, log *slog.Logger) (bot.Transport, error)

//nolint:ireturn // OpenClient's contract is the interface, so tests can supply a fake.
func openWhatsApp(ctx context.Context, database *sql.DB, log *slog.Logger) (bot.Transport, error) {
	client, err := whatsapp.Open(ctx, database, log)
	if err != nil {
		return nil, err //nolint:wrapcheck // run adds the context.
	}

	return client, nil
}

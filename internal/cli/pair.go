package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp"
)

func pair(ctx context.Context, cfg settings, log *slog.Logger, phone string) (err error) {
	database, err := sqlite.Open(ctx, cfg.shared.Store)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	defer func() {
		err = errors.Join(err, database.Close())
	}()

	return whatsapp.Pair(ctx, database, log, whatsapp.PairOptions{Phone: phone, In: os.Stdin, Out: os.Stdout}) //nolint:wrapcheck // Pair's errors already say what failed.
}

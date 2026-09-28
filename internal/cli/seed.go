package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/totallynotdavid/botkit/internal/auth"
)

func seedOwners(ctx context.Context, service *auth.Service, owners []string, log *slog.Logger) error {
	seeded, err := service.SeedOwners(ctx, owners)
	if err != nil {
		return fmt.Errorf("seed owners: %w", err)
	}

	for _, jid := range seeded.Created {
		log.InfoContext(ctx, "seeded owner", "jid", jid)
	}

	for _, jid := range seeded.Skipped {
		log.WarnContext(ctx, "owner already registered with another rank, left unchanged", "jid", jid)
	}

	for _, jid := range seeded.Inactive {
		log.WarnContext(ctx, "owner belongs to a deactivated user, left unchanged", "jid", jid)
	}

	return nil
}

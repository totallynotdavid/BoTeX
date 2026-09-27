package auth

import (
	"context"
	"errors"
	"fmt"
)

const (
	ownerRank          = "owner"
	systemRegisteredBy = "system"
)

// SeedOwnersResult reports what SeedOwners did for each requested JID.
type SeedOwnersResult struct {
	// Created lists JIDs that had no existing user and were registered as owner.
	Created []string
	// Skipped lists JIDs that already belong to a user with a different rank.
	// Their rank is left untouched: config never downgrades or silently
	// overwrites a rank assigned some other way.
	Skipped []string
}

// SeedOwners makes sure every JID in ownerJIDs is registered with the owner
// rank. It is idempotent: running it again with the same JIDs neither errors
// nor creates duplicate users. A JID that already belongs to a user with a
// different rank is left unchanged and reported in the result's Skipped
// field instead of being modified.
func (s *Service) SeedOwners(ctx context.Context, ownerJIDs []string) (*SeedOwnersResult, error) {
	result := &SeedOwnersResult{}

	for _, jid := range ownerJIDs {
		user, err := s.repo.GetUser(ctx, jid)
		if err != nil {
			if !errors.Is(err, ErrUserNotFound) {
				return nil, fmt.Errorf("failed to look up owner %q: %w", jid, err)
			}

			err = s.repo.CreateUser(ctx, jid, ownerRank, systemRegisteredBy)
			if err != nil {
				return nil, fmt.Errorf("failed to seed owner %q: %w", jid, err)
			}

			result.Created = append(result.Created, jid)

			continue
		}

		if user.Rank != ownerRank {
			result.Skipped = append(result.Skipped, jid)
		}
	}

	return result, nil
}

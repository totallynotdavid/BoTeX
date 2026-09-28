package auth

import (
	"context"
	"database/sql"
	"fmt"
)

const schema = `
-- Users table
CREATE TABLE IF NOT EXISTS users (
    user_id TEXT PRIMARY KEY,
    rank TEXT NOT NULL,
    registered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    registered_by TEXT,
    active INTEGER DEFAULT 1
);

-- Ranks table
CREATE TABLE IF NOT EXISTS ranks (
    name TEXT PRIMARY KEY,
    level INTEGER NOT NULL,
    commands TEXT NOT NULL,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    active INTEGER DEFAULT 1
);

-- Registered groups table
CREATE TABLE IF NOT EXISTS registered_groups (
    group_id TEXT PRIMARY KEY,
    registered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    registered_by TEXT NOT NULL,
    active INTEGER DEFAULT 1
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_users_active ON users(active);
CREATE INDEX IF NOT EXISTS idx_users_rank ON users(rank);
CREATE INDEX IF NOT EXISTS idx_ranks_active ON ranks(active);
CREATE INDEX IF NOT EXISTS idx_groups_active ON registered_groups(active);
`

const (
	// ownerRankLevel puts owner ahead of every rank an app defines.
	ownerRankLevel       = 0
	ownerRankDescription = "Bot owner with full access"
)

// initSchema creates the tables and inserts the owner rank plus defaults.
// Ranks that already exist are left as an operator edited them.
func initSchema(ctx context.Context, database *sql.DB, defaults []Rank) error {
	_, err := database.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}

	owner := Rank{Name: ownerRank, Level: ownerRankLevel, Commands: []string{"*"}, Description: ownerRankDescription}
	ranks := append([]Rank{owner}, defaults...)

	for _, rank := range ranks {
		err = ValidateRankName(rank.Name)
		if err != nil {
			return fmt.Errorf("default rank %q: %w", rank.Name, err)
		}

		_, err = database.ExecContext(ctx,
			`INSERT OR IGNORE INTO ranks (name, level, commands, description) VALUES (?, ?, ?, ?)`,
			rank.Name, rank.Level, JoinCommands(rank.Commands), rank.Description,
		)
		if err != nil {
			return fmt.Errorf("insert default rank %q: %w", rank.Name, err)
		}
	}

	return nil
}

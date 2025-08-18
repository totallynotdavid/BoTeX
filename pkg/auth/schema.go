package auth

import (
	"context"
	"database/sql"
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

const defaultRanksData = `
-- Insert default ranks
INSERT OR IGNORE INTO ranks (name, level, commands, description) VALUES
('owner', 0, '*', 'Bot owner with full access'),
('admin', 10, 'help,latex,register_user,register_group', 'Administrator with management access'),
('user', 100, 'help,latex', 'Basic user access');
`

func InitSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, defaultRanksData); err != nil {
		return err
	}

	return nil
}

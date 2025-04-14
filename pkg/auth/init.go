package auth

import (
	"context"
	"database/sql"
	"fmt"
)

func InitDB(ctx context.Context, database *sql.DB) error {
	if err := createTables(ctx, database); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	if err := insertDefaultData(ctx, database); err != nil {
		return fmt.Errorf("failed to insert default data: %w", err)
	}

	return nil
}

func createTables(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS botex_ranks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL,
			priority INTEGER NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT FALSE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create ranks table: %w", err)
	}

	_, err = database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS botex_permissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL,
			resource_cost INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create permissions table: %w", err)
	}

	_, err = database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS botex_rank_permissions (
			rank_id INTEGER NOT NULL,
			permission_id INTEGER NOT NULL,
			expires_at TIMESTAMP,
			PRIMARY KEY (rank_id, permission_id),
			FOREIGN KEY (rank_id) REFERENCES botex_ranks(id) ON DELETE CASCADE,
			FOREIGN KEY (permission_id) REFERENCES botex_permissions(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create rank_permissions table: %w", err)
	}

	_, err = database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS botex_users (
			jid TEXT PRIMARY KEY,
			rank_id INTEGER NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			FOREIGN KEY (rank_id) REFERENCES botex_ranks(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}

	return nil
}

func insertDefaultData(ctx context.Context, database *sql.DB) error {
	// Insert default ranks
	_, err := database.ExecContext(ctx, `
		INSERT OR IGNORE INTO botex_ranks (name, description, priority, is_default) VALUES
		('admin', 'Administrator with full access', 100, FALSE),
		('moderator', 'Moderator with elevated privileges', 50, FALSE),
		('user', 'Regular user with basic access', 0, TRUE)
	`)
	if err != nil {
		return fmt.Errorf("failed to insert default ranks: %w", err)
	}

	// Insert default permissions
	_, err = database.ExecContext(ctx, `
		INSERT OR IGNORE INTO botex_permissions (name, description, resource_cost) VALUES
		('manage_ranks', 'Ability to manage user ranks', 10),
		('manage_permissions', 'Ability to manage permissions', 10),
		('use_latex', 'Ability to use LaTeX commands', 1),
		('send_messages', 'Ability to send messages', 0)
	`)
	if err != nil {
		return fmt.Errorf("failed to insert default permissions: %w", err)
	}

	// Insert default rank permissions
	_, err = database.ExecContext(ctx, `
		INSERT OR IGNORE INTO botex_rank_permissions (rank_id, permission_id) 
		SELECT r.id, p.id
		FROM botex_ranks r
		CROSS JOIN botex_permissions p
		WHERE r.name = 'admin'
		OR (r.name = 'moderator' AND p.name IN ('use_latex', 'send_messages'))
		OR (r.name = 'user' AND p.name = 'send_messages')
	`)
	if err != nil {
		return fmt.Errorf("failed to insert default rank permissions: %w", err)
	}

	return nil
}

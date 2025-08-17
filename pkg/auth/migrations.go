package auth

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration represents a database migration with version and SQL statements
type Migration struct {
	Version int
	Name    string
	Up      []string
	Down    []string
}

// GetAuthMigrations returns all auth module database migrations
func GetAuthMigrations() []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "create_unified_users_table",
			Up: []string{
				`CREATE TABLE IF NOT EXISTS users (
					user_id TEXT PRIMARY KEY,
					rank TEXT NOT NULL DEFAULT 'basic',
					registered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					registered_by TEXT,
					active BOOLEAN NOT NULL DEFAULT TRUE,
					FOREIGN KEY (rank) REFERENCES ranks(name)
				)`,
				`CREATE INDEX IF NOT EXISTS idx_users_rank ON users(rank) WHERE active = TRUE`,
				`CREATE INDEX IF NOT EXISTS idx_users_active ON users(active)`,
			},
			Down: []string{
				`DROP INDEX IF EXISTS idx_users_active`,
				`DROP INDEX IF EXISTS idx_users_rank`,
				`DROP TABLE IF EXISTS users`,
			},
		},
		{
			Version: 2,
			Name:    "create_ranks_table",
			Up: []string{
				`CREATE TABLE IF NOT EXISTS ranks (
					name TEXT PRIMARY KEY,
					level INTEGER NOT NULL,
					commands TEXT NOT NULL DEFAULT '[]',
					description TEXT,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					active BOOLEAN NOT NULL DEFAULT TRUE
				)`,
				`CREATE INDEX IF NOT EXISTS idx_ranks_level ON ranks(level) WHERE active = TRUE`,
				`CREATE INDEX IF NOT EXISTS idx_ranks_active ON ranks(active)`,
			},
			Down: []string{
				`DROP INDEX IF EXISTS idx_ranks_active`,
				`DROP INDEX IF EXISTS idx_ranks_level`,
				`DROP TABLE IF EXISTS ranks`,
			},
		},
		{
			Version: 3,
			Name:    "create_registered_groups_table",
			Up: []string{
				`CREATE TABLE IF NOT EXISTS registered_groups (
					group_id TEXT PRIMARY KEY,
					registered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					registered_by TEXT NOT NULL,
					active BOOLEAN NOT NULL DEFAULT TRUE,
					FOREIGN KEY (registered_by) REFERENCES users(user_id)
				)`,
				`CREATE INDEX IF NOT EXISTS idx_registered_groups_active ON registered_groups(active)`,
				`CREATE INDEX IF NOT EXISTS idx_registered_groups_by ON registered_groups(registered_by)`,
			},
			Down: []string{
				`DROP INDEX IF EXISTS idx_registered_groups_by`,
				`DROP INDEX IF EXISTS idx_registered_groups_active`,
				`DROP TABLE IF EXISTS registered_groups`,
			},
		},
	}
}

// MigrateAuth runs all auth module migrations on the provided database connection
func MigrateAuth(ctx context.Context, db *sql.DB) error {
	// Create migration tracking table if it doesn't exist
	err := createMigrationTable(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	migrations := GetAuthMigrations()
	
	for _, migration := range migrations {
		applied, err := isMigrationApplied(ctx, db, "auth", migration.Version)
		if err != nil {
			return fmt.Errorf("failed to check migration status for %s: %w", migration.Name, err)
		}

		if applied {
			continue
		}

		err = applyMigration(ctx, db, "auth", migration)
		if err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", migration.Name, err)
		}
	}

	return nil
}

// RollbackAuth rolls back auth module migrations to a specific version
func RollbackAuth(ctx context.Context, db *sql.DB, targetVersion int) error {
	migrations := GetAuthMigrations()
	
	// Apply rollbacks in reverse order
	for i := len(migrations) - 1; i >= 0; i-- {
		migration := migrations[i]
		
		if migration.Version <= targetVersion {
			break
		}

		applied, err := isMigrationApplied(ctx, db, "auth", migration.Version)
		if err != nil {
			return fmt.Errorf("failed to check migration status for %s: %w", migration.Name, err)
		}

		if !applied {
			continue
		}

		err = rollbackMigration(ctx, db, "auth", migration)
		if err != nil {
			return fmt.Errorf("failed to rollback migration %s: %w", migration.Name, err)
		}
	}

	return nil
}

// createMigrationTable creates the migration tracking table
func createMigrationTable(ctx context.Context, db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			module TEXT NOT NULL,
			version INTEGER NOT NULL,
			name TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (module, version)
		)
	`
	
	_, err := db.ExecContext(ctx, query)
	return err
}

// isMigrationApplied checks if a specific migration has been applied
func isMigrationApplied(ctx context.Context, db *sql.DB, module string, version int) (bool, error) {
	query := `SELECT COUNT(*) FROM schema_migrations WHERE module = ? AND version = ?`
	
	var count int
	err := db.QueryRowContext(ctx, query, module, version).Scan(&count)
	if err != nil {
		return false, err
	}
	
	return count > 0, nil
}

// applyMigration applies a single migration
func applyMigration(ctx context.Context, db *sql.DB, module string, migration Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute all UP statements
	for _, stmt := range migration.Up {
		_, err = tx.ExecContext(ctx, stmt)
		if err != nil {
			return fmt.Errorf("failed to execute migration statement: %w", err)
		}
	}

	// Record the migration as applied
	_, err = tx.ExecContext(ctx, 
		`INSERT INTO schema_migrations (module, version, name) VALUES (?, ?, ?)`,
		module, migration.Version, migration.Name)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

// rollbackMigration rolls back a single migration
func rollbackMigration(ctx context.Context, db *sql.DB, module string, migration Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute all DOWN statements
	for _, stmt := range migration.Down {
		_, err = tx.ExecContext(ctx, stmt)
		if err != nil {
			return fmt.Errorf("failed to execute rollback statement: %w", err)
		}
	}

	// Remove the migration record
	_, err = tx.ExecContext(ctx, 
		`DELETE FROM schema_migrations WHERE module = ? AND version = ?`,
		module, migration.Version)
	if err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}

	return tx.Commit()
}

// GetAppliedMigrations returns a list of applied migrations for the auth module
func GetAppliedMigrations(ctx context.Context, db *sql.DB) ([]Migration, error) {
	query := `
		SELECT version, name 
		FROM schema_migrations 
		WHERE module = 'auth' 
		ORDER BY version ASC
	`
	
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var applied []Migration
	for rows.Next() {
		var migration Migration
		err = rows.Scan(&migration.Version, &migration.Name)
		if err != nil {
			return nil, err
		}
		applied = append(applied, migration)
	}

	return applied, rows.Err()
}

// InitializeFreshSchema creates a fresh database schema with default data
// This function performs the complete database setup process:
// 1. Runs all auth migrations to create tables and indexes
// 2. Initializes default ranks (owner and basic) with empty command lists
// 3. Validates the schema to ensure everything is properly set up
func InitializeFreshSchema(ctx context.Context, db *sql.DB) error {
	// Run all migrations first
	err := MigrateAuth(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Initialize default ranks
	err = initializeDefaultRanks(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to initialize default ranks: %w", err)
	}

	// Validate the schema
	err = ValidateAuthSchema(ctx, db)
	if err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	return nil
}

// InitializeFreshSchemaWithConfig creates a fresh database schema with configuration-based defaults
func InitializeFreshSchemaWithConfig(ctx context.Context, db *sql.DB, defaultRank string) error {
	// Run all migrations first
	err := MigrateAuth(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Initialize default ranks with custom default rank
	err = initializeDefaultRanksWithConfig(ctx, db, defaultRank)
	if err != nil {
		return fmt.Errorf("failed to initialize default ranks: %w", err)
	}

	// Validate the schema
	err = ValidateAuthSchema(ctx, db)
	if err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	return nil
}

// initializeDefaultRanks creates the default owner and basic ranks
func initializeDefaultRanks(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, rank := range DefaultRanks {
		// Check if rank already exists
		var count int
		err = tx.QueryRowContext(ctx, 
			`SELECT COUNT(*) FROM ranks WHERE name = ?`, rank.Name).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check existing rank %s: %w", rank.Name, err)
		}

		if count > 0 {
			continue // Rank already exists
		}

		// Marshal commands to JSON
		err = rank.MarshalCommands()
		if err != nil {
			return fmt.Errorf("failed to marshal commands for rank %s: %w", rank.Name, err)
		}

		// Insert the rank
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ranks (name, level, commands, description, active) 
			VALUES (?, ?, ?, ?, ?)`,
			rank.Name, rank.Level, rank.CommandsRaw, rank.Description, rank.Active)
		if err != nil {
			return fmt.Errorf("failed to insert rank %s: %w", rank.Name, err)
		}
	}

	return tx.Commit()
}

// initializeDefaultRanksWithConfig creates the default ranks with custom configuration
func initializeDefaultRanksWithConfig(ctx context.Context, db *sql.DB, defaultRank string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create default ranks with the specified default rank
	defaultRanks := []*Rank{
		{
			Name:        "owner",
			Level:       0,
			Commands:    []string{}, // Empty - owner must manually configure
			Description: "Bot owner with full administrative access",
			Active:      true,
		},
		{
			Name:        defaultRank,
			Level:       100,
			Commands:    []string{}, // Empty - owner must manually configure
			Description: fmt.Sprintf("Default access level for users (%s)", defaultRank),
			Active:      true,
		},
	}

	// If default rank is not "basic", also create basic rank
	if defaultRank != "basic" {
		defaultRanks = append(defaultRanks, &Rank{
			Name:        "basic",
			Level:       200,
			Commands:    []string{}, // Empty - owner must manually configure
			Description: "Basic access level for users",
			Active:      true,
		})
	}

	for _, rank := range defaultRanks {
		// Check if rank already exists
		var count int
		err = tx.QueryRowContext(ctx, 
			`SELECT COUNT(*) FROM ranks WHERE name = ?`, rank.Name).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check existing rank %s: %w", rank.Name, err)
		}

		if count > 0 {
			continue // Rank already exists
		}

		// Marshal commands to JSON
		err = rank.MarshalCommands()
		if err != nil {
			return fmt.Errorf("failed to marshal commands for rank %s: %w", rank.Name, err)
		}

		// Insert the rank
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ranks (name, level, commands, description, active) 
			VALUES (?, ?, ?, ?, ?)`,
			rank.Name, rank.Level, rank.CommandsRaw, rank.Description, rank.Active)
		if err != nil {
			return fmt.Errorf("failed to insert rank %s: %w", rank.Name, err)
		}
	}

	return tx.Commit()
}

// ValidateAuthSchema validates that the auth database schema is properly created
func ValidateAuthSchema(ctx context.Context, db *sql.DB) error {
	// Check if users table exists with correct structure
	err := validateTable(ctx, db, "users", []string{
		"user_id", "rank", "registered_at", "registered_by", "active",
	})
	if err != nil {
		return fmt.Errorf("users table validation failed: %w", err)
	}

	// Check if ranks table exists with correct structure
	err = validateTable(ctx, db, "ranks", []string{
		"name", "level", "commands", "description", "created_at", "active",
	})
	if err != nil {
		return fmt.Errorf("ranks table validation failed: %w", err)
	}

	// Check if registered_groups table exists with correct structure
	err = validateTable(ctx, db, "registered_groups", []string{
		"group_id", "registered_at", "registered_by", "active",
	})
	if err != nil {
		return fmt.Errorf("registered_groups table validation failed: %w", err)
	}

	// Check if indexes exist
	indexes := []string{
		"idx_users_rank",
		"idx_users_active",
		"idx_ranks_level",
		"idx_ranks_active",
		"idx_registered_groups_active", 
		"idx_registered_groups_by",
	}

	for _, index := range indexes {
		exists, err := indexExists(ctx, db, index)
		if err != nil {
			return fmt.Errorf("failed to check index %s: %w", index, err)
		}
		if !exists {
			return fmt.Errorf("index %s does not exist", index)
		}
	}

	// Validate that default ranks exist
	err = validateDefaultRanks(ctx, db)
	if err != nil {
		return fmt.Errorf("default ranks validation failed: %w", err)
	}

	return nil
}

// validateDefaultRanks ensures that the default ranks are present in the database
func validateDefaultRanks(ctx context.Context, db *sql.DB) error {
	for _, defaultRank := range DefaultRanks {
		var count int
		err := db.QueryRowContext(ctx, 
			`SELECT COUNT(*) FROM ranks WHERE name = ? AND active = TRUE`, 
			defaultRank.Name).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check default rank %s: %w", defaultRank.Name, err)
		}
		
		if count == 0 {
			return fmt.Errorf("default rank %s not found or inactive", defaultRank.Name)
		}
	}
	return nil
}

// validateTable checks if a table exists and has the expected columns
func validateTable(ctx context.Context, db *sql.DB, tableName string, expectedColumns []string) error {
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name=?`
	
	var name string
	err := db.QueryRowContext(ctx, query, tableName).Scan(&name)
	if err == sql.ErrNoRows {
		return fmt.Errorf("table %s does not exist", tableName)
	}
	if err != nil {
		return err
	}

	// Check columns
	pragmaQuery := fmt.Sprintf("PRAGMA table_info(%s)", tableName)
	rows, err := db.QueryContext(ctx, pragmaQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var actualColumns []string
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString
		
		err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			return err
		}
		actualColumns = append(actualColumns, name)
	}

	// Verify all expected columns exist
	for _, expected := range expectedColumns {
		found := false
		for _, actual := range actualColumns {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("column %s not found in table %s", expected, tableName)
		}
	}

	return nil
}

// indexExists checks if an index exists in the database
func indexExists(ctx context.Context, db *sql.DB, indexName string) (bool, error) {
	query := `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`
	
	var count int
	err := db.QueryRowContext(ctx, query, indexName).Scan(&count)
	if err != nil {
		return false, err
	}
	
	return count > 0, nil
}
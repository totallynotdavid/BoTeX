// Package sqlite opens the SQLite database both bots keep their session,
// users and state in, using the pure-Go modernc.org/sqlite driver.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// busyTimeout is how long, in milliseconds, a connection waits for another
// connection's write lock before failing with SQLITE_BUSY.
const busyTimeout = 5000

// Open opens or creates the database file at path. Every connection has
// foreign keys on, which whatsmeow's store requires, and write-ahead logging,
// so readers never block the writer.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout))
	// A write transaction takes the lock when it begins, so two writers wait
	// on busy_timeout instead of one failing when it upgrades from reading.
	query.Set("_txlock", "immediate")

	dsn := (&url.URL{Scheme: "file", Opaque: url.PathEscape(path), RawQuery: query.Encode()}).String()

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	err = database.PingContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, errors.Join(err, database.Close()))
	}

	return database, nil
}

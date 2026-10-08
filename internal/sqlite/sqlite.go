// Package sqlite opens the SQLite database both bots keep their session,
// users and state in, using the pure-Go modernc.org/sqlite driver.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"slices"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// busyTimeout is how long, in milliseconds, a connection waits for another
// connection's write lock before failing with SQLITE_BUSY.
const busyTimeout = 5000

// ErrNoStore is returned by OpenExisting when the database file does not exist.
var ErrNoStore = errors.New("store does not exist")

// Option changes how Open sets up the database.
type Option func(query url.Values)

// WithoutSync stops commits waiting for the disk. A crash or power cut can
// lose the latest writes, so callers should use it only for tests.
func WithoutSync() Option {
	return func(query url.Values) { query.Add("_pragma", "synchronous(OFF)") }
}

// Open opens or creates the database file at path. Every connection has
// foreign keys on, which whatsmeow's store requires, and write-ahead logging,
// so readers never block the writer.
func Open(ctx context.Context, path string, opts ...Option) (*sql.DB, error) {
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout))
	// A write transaction takes the lock when it begins, so two writers wait
	// on busy_timeout instead of one failing when it upgrades from reading.
	query.Set("_txlock", "immediate")

	for _, opt := range opts {
		opt(query)
	}

	dsn := (&url.URL{Scheme: "file", Opaque: url.PathEscape(path), RawQuery: query.Encode()}).String()

	return connect(ctx, path, dsn)
}

// OpenExisting opens the database file at path only if it exists. A command
// run from the wrong directory or without the bot's environment then fails
// with ErrNoStore instead of working on a new, empty database.
func OpenExisting(ctx context.Context, path string, opts ...Option) (*sql.DB, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNoStore, path)
	}

	// The mode keeps SQLite from creating the file if it vanishes after the
	// check above.
	readWrite := func(query url.Values) { query.Set("mode", "rw") }

	return Open(ctx, path, slices.Concat(opts, []Option{readWrite})...)
}

func connect(ctx context.Context, path, dsn string) (*sql.DB, error) {
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

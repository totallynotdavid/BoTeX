package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/totallynotdavid/botkit/internal/sqlite"
)

func open(t *testing.T, path string, opts ...sqlite.Option) *sql.DB {
	t.Helper()

	database, err := sqlite.Open(t.Context(), path, opts...)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	return database
}

// pragmaPerConnection returns what PRAGMA name reports on each of several
// connections. A pool opens connections lazily, so they are held at once to
// make it open them.
func pragmaPerConnection(t *testing.T, database *sql.DB, name string) []string {
	t.Helper()

	const connections = 3

	values := make([]string, 0, connections)

	for range connections {
		conn, err := database.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			closeErr := conn.Close()
			if closeErr != nil {
				t.Errorf("close connection: %v", closeErr)
			}
		})

		var got string

		err = conn.QueryRowContext(t.Context(), "PRAGMA "+name).Scan(&got)
		if err != nil {
			t.Fatalf("PRAGMA %s: %v", name, err)
		}

		values = append(values, got)
	}

	return values
}

func requirePragma(t *testing.T, database *sql.DB, name, want string) {
	t.Helper()

	for i, got := range pragmaPerConnection(t, database, name) {
		if got != want {
			t.Errorf("conn %d: PRAGMA %s = %q, want %q", i, name, got, want)
		}
	}
}

func TestOpenSetsPragmasOnEveryConnection(t *testing.T) {
	t.Parallel()

	database := open(t, filepath.Join(t.TempDir(), "bot.db"))

	requirePragma(t, database, "foreign_keys", "1")
	requirePragma(t, database, "journal_mode", "wal")
	requirePragma(t, database, "busy_timeout", strconv.Itoa(sqlite.BusyTimeout))
}

// Commits wait for the disk unless the caller opts out, and the opt-out holds
// on every connection.
func TestOpenSyncsCommitsUnlessToldNot(t *testing.T) {
	t.Parallel()

	const (
		off  = "0"
		full = "2"
	)

	requirePragma(t, open(t, filepath.Join(t.TempDir(), "synced.db")), "synchronous", full)
	requirePragma(t, open(t, filepath.Join(t.TempDir(), "unsynced.db"), sqlite.WithoutSync()), "synchronous", off)
}

func TestOpenPathsNeedingEscapes(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"plain.db", "with space.db", "hash#and?query.db", "percent%20.db"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), name)
			database := open(t, path)

			_, err := database.ExecContext(t.Context(), "CREATE TABLE t (x INTEGER)")
			if err != nil {
				t.Fatal(err)
			}

			_, err = os.Stat(path)
			if err != nil {
				t.Errorf("database not created at the given path: %v", err)
			}
		})
	}
}

//nolint:paralleltest // t.Chdir changes the process's directory.
func TestOpenRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	open(t, "rel.db")

	_, err := os.Stat(filepath.Join(dir, "rel.db"))
	if err != nil {
		t.Errorf("relative path not resolved against the working directory: %v", err)
	}
}

func TestOpenMissingDirFails(t *testing.T) {
	t.Parallel()

	_, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "missing", "bot.db"))
	if err == nil {
		t.Fatal("Open succeeded in a directory that does not exist")
	}
}

func TestConcurrentWritersWait(t *testing.T) {
	t.Parallel()

	database := open(t, filepath.Join(t.TempDir(), "bot.db"))

	_, err := database.ExecContext(t.Context(), "CREATE TABLE n (v INTEGER)")
	if err != nil {
		t.Fatal(err)
	}

	const writers = 8

	errs := make(chan error, writers)

	for range writers {
		go func() { errs <- readThenWrite(t.Context(), database) }()
	}

	for range writers {
		err := <-errs
		if err != nil {
			t.Errorf("writer failed instead of waiting for the lock: %v", err)
		}
	}
}

// readThenWrite is the pattern that fails with SQLITE_BUSY when a transaction
// only takes the write lock at its first write.
func readThenWrite(ctx context.Context, database *sql.DB) error {
	txn, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	var count int

	err = txn.QueryRowContext(ctx, "SELECT count(*) FROM n").Scan(&count)
	if err == nil {
		_, err = txn.ExecContext(ctx, "INSERT INTO n (v) VALUES (?)", count)
	}

	if err != nil {
		return fmt.Errorf("read then write: %w", errors.Join(err, txn.Rollback()))
	}

	err = txn.Commit()
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

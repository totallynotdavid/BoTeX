package whatsapp_test

import (
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp"
)

// notTerminal is what stdin and stdout are under systemd or a pipe.
func notTerminal(t *testing.T) *os.File {
	t.Helper()

	file, err := os.Create(filepath.Join(t.TempDir(), "stream"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := file.Close()
		if closeErr != nil {
			t.Errorf("close stream: %v", closeErr)
		}
	})

	return file
}

// untouched fails the test if anything created a table in database. Opening a
// whatsapp client creates whatsmeow's tables, so an empty database proves
// Pair stopped before it built a client, let alone dialled.
func untouched(t *testing.T, database *sql.DB) {
	t.Helper()

	var tables int

	err := database.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master").Scan(&tables)
	if err != nil {
		t.Fatal(err)
	}

	if tables != 0 {
		t.Errorf("Pair created %d tables, want none: it must refuse before opening the store", tables)
	}
}

func TestPairRefusals(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		phone string
		want  error
	}{
		"QR without a terminal":            {want: whatsapp.ErrNotTerminal},
		"pairing code without a terminal":  {phone: "+51900000000", want: whatsapp.ErrNotTerminal},
		"phone without the plus":           {phone: "51900000000", want: whatsapp.ErrInvalidPhone},
		"phone with letters":               {phone: "+5190000abcd", want: whatsapp.ErrInvalidPhone},
		"phone too short to be a number":   {phone: "+519", want: whatsapp.ErrInvalidPhone},
		"phone with separators inside":     {phone: "+51 900 000 000", want: whatsapp.ErrInvalidPhone},
		"phone longer than any E.164 form": {phone: "+1234567890123456", want: whatsapp.ErrInvalidPhone},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "bot.db"))
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				closeErr := database.Close()
				if closeErr != nil {
					t.Errorf("close database: %v", closeErr)
				}
			})

			err = whatsapp.Pair(t.Context(), database, slog.New(slog.DiscardHandler), whatsapp.PairOptions{
				Phone: test.phone,
				In:    notTerminal(t),
				Out:   notTerminal(t),
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("Pair() error = %v, want %v", err, test.want)
			}

			untouched(t, database)
		})
	}
}

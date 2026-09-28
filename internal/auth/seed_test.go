package auth_test

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/sqlite"
)

const (
	rankAdmin = "admin"
	rankUser  = "user"

	cmdHelp  = "help"
	cmdLatex = "latex"
)

func testRanks() []auth.Rank {
	return []auth.Rank{
		{Name: rankAdmin, Level: 10, Commands: []string{cmdHelp, cmdLatex}, Description: "Administrator with management access"},
		{Name: rankUser, Level: 100, Commands: []string{cmdHelp}, Description: "Basic user access"},
	}
}

func newService(t *testing.T) (*auth.Service, *sql.DB) {
	t.Helper()

	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	service, err := auth.New(t.Context(), database, testRanks()...)
	if err != nil {
		t.Fatal(err)
	}

	return service, database
}

func TestSeedOwnersGrantsFullAccess(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	const ownerJID = "15551234567@s.whatsapp.net"

	_, err := service.SeedOwners(t.Context(), []string{ownerJID})
	if err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{cmdHelp, cmdLatex, "register_user", "anything"} {
		decision, err := service.Authorize(t.Context(), ownerJID, "", command)
		if err != nil {
			t.Fatalf("Authorize(%q): %v", command, err)
		}

		if decision != auth.Allowed {
			t.Errorf("owner denied %q: %v", command, decision)
		}
	}
}

func TestSeedOwnersIsIdempotent(t *testing.T) {
	t.Parallel()

	service, database := newService(t)

	const ownerJID = "15551234567@s.whatsapp.net"

	first, err := service.SeedOwners(t.Context(), []string{ownerJID})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(first.Created, []string{ownerJID}) {
		t.Fatalf("first seed created %v", first.Created)
	}

	second, err := service.SeedOwners(t.Context(), []string{ownerJID})
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Created) != 0 || len(second.Skipped) != 0 || len(second.Inactive) != 0 {
		t.Errorf("second seed changed something: %+v", second)
	}

	var count int

	err = database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM users WHERE user_id = ?", ownerJID).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("got %d rows for %q, want 1", count, ownerJID)
	}
}

func TestSeedOwnersLeavesInactiveUserUntouched(t *testing.T) {
	t.Parallel()

	service, database := newService(t)

	const inactiveJID = "15550001111@s.whatsapp.net"

	_, err := database.ExecContext(t.Context(),
		"INSERT INTO users (user_id, rank, registered_by, active) VALUES (?, ?, ?, 0)",
		inactiveJID, rankAdmin, "someone",
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.SeedOwners(t.Context(), []string{inactiveJID})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Created) != 0 || len(result.Skipped) != 0 {
		t.Errorf("seed changed an inactive user: %+v", result)
	}

	if !slices.Equal(result.Inactive, []string{inactiveJID}) {
		t.Errorf("inactive = %v, want %q", result.Inactive, inactiveJID)
	}

	var (
		rank   string
		active int
	)

	err = database.QueryRowContext(t.Context(), "SELECT rank, active FROM users WHERE user_id = ?", inactiveJID).Scan(&rank, &active)
	if err != nil {
		t.Fatal(err)
	}

	if rank != rankAdmin || active != 0 {
		t.Errorf("user changed to rank=%q active=%d", rank, active)
	}
}

func TestSeedOwnersDoesNotDowngradeExistingRank(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	const existingJID = "15559876543@s.whatsapp.net"

	err := service.RegisterUser(t.Context(), existingJID, rankAdmin, "someone")
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.SeedOwners(t.Context(), []string{existingJID})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Created) != 0 || !slices.Equal(result.Skipped, []string{existingJID}) {
		t.Errorf("seed result = %+v, want %q skipped", result, existingJID)
	}

	user, err := service.GetUser(t.Context(), existingJID)
	if err != nil {
		t.Fatal(err)
	}

	if user.Rank != rankAdmin {
		t.Errorf("rank = %q, want admin", user.Rank)
	}
}

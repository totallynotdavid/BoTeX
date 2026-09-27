package auth_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"botex/pkg/auth"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("failed to close database: %v", closeErr)
		}
	})

	err = auth.InitSchema(context.Background(), database)
	if err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	return database
}

func TestSeedOwners_GrantsFullAccess(t *testing.T) {
	t.Parallel()

	database := newTestDB(t)
	service := auth.NewService(database)
	ctx := context.Background()

	const ownerJID = "15551234567@s.whatsapp.net"

	_, err := service.SeedOwners(ctx, []string{ownerJID})
	if err != nil {
		t.Fatalf("SeedOwners failed: %v", err)
	}

	for _, command := range []string{"help", "latex", "register_user", "anything"} {
		result, err := service.CheckPermission(ctx, ownerJID, "", command)
		if err != nil {
			t.Fatalf("CheckPermission(%q) failed: %v", command, err)
		}

		if !result.Allowed {
			t.Errorf("expected owner to be allowed to run %q, got denied: %s", command, result.Reason)
		}
	}
}

func TestSeedOwners_IdempotentNoDuplicates(t *testing.T) {
	t.Parallel()

	database := newTestDB(t)
	service := auth.NewService(database)
	ctx := context.Background()

	const ownerJID = "15551234567@s.whatsapp.net"

	_, err := service.SeedOwners(ctx, []string{ownerJID})
	if err != nil {
		t.Fatalf("first SeedOwners failed: %v", err)
	}

	result, err := service.SeedOwners(ctx, []string{ownerJID})
	if err != nil {
		t.Fatalf("second SeedOwners failed: %v", err)
	}

	if len(result.Created) != 0 {
		t.Errorf("expected no new users created on second seed, got %v", result.Created)
	}

	var count int

	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE user_id = ?", ownerJID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count users: %v", err)
	}

	if count != 1 {
		t.Fatalf("expected exactly one row for %q, got %d", ownerJID, count)
	}
}

func TestSeedOwners_DoesNotDowngradeExistingRank(t *testing.T) {
	t.Parallel()

	database := newTestDB(t)
	service := auth.NewService(database)
	ctx := context.Background()

	const existingJID = "15559876543@s.whatsapp.net"

	err := service.RegisterUser(ctx, existingJID, "admin", "someone")
	if err != nil {
		t.Fatalf("failed to register existing user: %v", err)
	}

	result, err := service.SeedOwners(ctx, []string{existingJID})
	if err != nil {
		t.Fatalf("SeedOwners failed: %v", err)
	}

	if len(result.Created) != 0 {
		t.Errorf("expected no users created, got %v", result.Created)
	}

	if len(result.Skipped) != 1 || result.Skipped[0] != existingJID {
		t.Errorf("expected %q to be reported as skipped, got %v", existingJID, result.Skipped)
	}

	user, err := service.GetUser(ctx, existingJID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	if user.Rank != "admin" {
		t.Errorf("expected rank to remain %q, got %q", "admin", user.Rank)
	}
}

package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/pkg/commands"
)

var errDatabase = errors.New("database is locked")

// denyingAuth denies every command, then answers the lookup that explains the
// denial. A real service cannot fail the second query after the first one
// passed, so the lookup's outcome is set directly.
type denyingAuth struct {
	user    *auth.User
	userErr error
}

func (denyingAuth) CheckPermission(context.Context, string, string) (bool, error) {
	return false, nil
}

func (a denyingAuth) GetUser(context.Context, string) (*auth.User, error) {
	return a.user, a.userErr
}

func (denyingAuth) GetGroup(context.Context, string) (*auth.Group, error) {
	return &auth.Group{}, nil
}

func TestDenyReasonExplainsADeniedCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		auth denyingAuth
		want string
	}{
		{"unregistered user", denyingAuth{userErr: auth.ErrUserNotFound}, "User not registered"},
		{"rank without the command", denyingAuth{user: &auth.User{ID: "u", Rank: "basic"}}, "Command not allowed for your rank"},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			got, err := commands.DenyReason(t.Context(), scenario.auth, "u", "", "latex")
			if err != nil {
				t.Fatal(err)
			}

			if got != scenario.want {
				t.Errorf("reason = %q, want %q", got, scenario.want)
			}
		})
	}
}

func TestDenyReasonSurfacesALookupFailure(t *testing.T) {
	t.Parallel()

	reason, err := commands.DenyReason(t.Context(), denyingAuth{userErr: errDatabase}, "u", "", "latex")
	if !errors.Is(err, errDatabase) {
		t.Fatalf("DenyReason = %q, %v, want the lookup error", reason, err)
	}

	if reason != "" {
		t.Errorf("reason = %q, want none when the lookup failed", reason)
	}
}

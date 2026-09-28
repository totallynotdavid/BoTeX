package auth_test

import (
	"errors"
	"maps"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
)

func TestAuthorize(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	const (
		admin           = "15550000001@s.whatsapp.net"
		user            = "15550000002@s.whatsapp.net"
		unknown         = "15550000003@s.whatsapp.net"
		registeredGroup = "120363000000000001@g.us"
		otherGroup      = "120363000000000002@g.us"
	)

	for jid, rank := range map[string]string{admin: rankAdmin, user: rankUser} {
		err := service.RegisterUser(t.Context(), jid, rank, "system")
		if err != nil {
			t.Fatal(err)
		}
	}

	err := service.RegisterGroup(t.Context(), registeredGroup, admin)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		jid     string
		group   string
		command string
		want    auth.Decision
	}{
		{"rank lists command", admin, "", cmdLatex, auth.Allowed},
		{"rank lacks command", user, "", cmdLatex, auth.RankLacksCommand},
		{"rank lists help", user, "", cmdHelp, auth.Allowed},
		{"unregistered user", unknown, "", cmdHelp, auth.UserNotRegistered},
		{"registered group", user, registeredGroup, cmdHelp, auth.Allowed},
		{"unregistered group", user, otherGroup, cmdHelp, auth.GroupNotRegistered},
		{"unregistered user beats unregistered group", unknown, otherGroup, cmdHelp, auth.UserNotRegistered},
		{"rank beats unregistered group", user, otherGroup, cmdLatex, auth.RankLacksCommand},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			got, err := service.Authorize(t.Context(), scenario.jid, scenario.group, scenario.command)
			if err != nil {
				t.Fatal(err)
			}

			if got != scenario.want {
				t.Fatalf("Authorize(%q, %q, %q) = %v, want %v", scenario.jid, scenario.group, scenario.command, got, scenario.want)
			}
		})
	}
}

func TestAuthorizeRejectsInvalidCommand(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	for _, command := range []string{"", "no spaces", "semi;colon"} {
		_, err := service.Authorize(t.Context(), "15550000001@s.whatsapp.net", "", command)
		if !errors.Is(err, auth.ErrInvalidInput) {
			t.Errorf("Authorize(%q) error = %v, want ErrInvalidInput", command, err)
		}
	}
}

// A lookup that fails must not grant the command: the decision is not Allowed
// and the legacy bool is false, so a caller reading either one first is safe.
func TestAuthorizeFailsClosedWhenTheLookupFails(t *testing.T) {
	t.Parallel()

	service, database := newService(t)

	const admin = "15550000001@s.whatsapp.net"

	err := service.RegisterUser(t.Context(), admin, rankAdmin, "system")
	if err != nil {
		t.Fatal(err)
	}

	err = database.Close()
	if err != nil {
		t.Fatal(err)
	}

	decision, err := service.Authorize(t.Context(), admin, "", cmdLatex)
	if err == nil {
		t.Fatal("Authorize on a closed database succeeded, want an error")
	}

	if decision == auth.Allowed {
		t.Errorf("Authorize decision = Allowed with error %v", err)
	}

	if decision != auth.Undecided {
		t.Errorf("Authorize decision = %v, want Undecided", decision)
	}
}

func TestNewStoresDefaultRanksOnce(t *testing.T) {
	t.Parallel()

	service, database := newService(t)

	_, err := database.ExecContext(t.Context(), "UPDATE ranks SET commands = 'edited' WHERE name = 'user'")
	if err != nil {
		t.Fatal(err)
	}

	_, err = auth.New(t.Context(), database, testRanks()...)
	if err != nil {
		t.Fatal(err)
	}

	ranks, err := service.ListRanks(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]*auth.Rank{}
	for _, rank := range ranks {
		got[rank.Name] = rank
	}

	if len(got) != 3 || got["owner"].Level != 0 || got["admin"].Level != 10 {
		t.Fatalf("ranks = %+v", got)
	}

	if !got["user"].HasCommand("edited") {
		t.Errorf("second New overwrote the stored rank: %+v", got["user"])
	}
}

func TestNewDescribesEveryRankOnAFreshDatabase(t *testing.T) {
	t.Parallel()

	service, database := newService(t)

	ranks, err := service.ListRanks(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"owner": "Bot owner with full access",
		"admin": "Administrator with management access",
		"user":  "Basic user access",
	}

	got := map[string]string{}
	for _, rank := range ranks {
		got[rank.Name] = rank.Description
	}

	if !maps.Equal(got, want) {
		t.Errorf("rank descriptions = %v, want %v", got, want)
	}

	var stored int

	err = database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM ranks WHERE description IS NULL").Scan(&stored)
	if err != nil {
		t.Fatal(err)
	}

	if stored != 0 {
		t.Errorf("%d ranks were stored with a NULL description", stored)
	}
}

func TestNewRejectsInvalidRankName(t *testing.T) {
	t.Parallel()

	_, database := newService(t)

	_, err := auth.New(t.Context(), database, auth.Rank{Name: "Bad Name"})
	if !errors.Is(err, auth.ErrInvalidInput) {
		t.Fatalf("New error = %v, want ErrInvalidInput", err)
	}
}

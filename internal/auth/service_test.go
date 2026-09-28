package auth_test

import (
	"errors"
	"maps"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
)

func TestCheckPermission(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	const (
		admin   = "15550000001@s.whatsapp.net"
		user    = "15550000002@s.whatsapp.net"
		unknown = "15550000003@s.whatsapp.net"
	)

	for jid, rank := range map[string]string{admin: rankAdmin, user: rankUser} {
		err := service.RegisterUser(t.Context(), jid, rank, "system")
		if err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name    string
		jid     string
		command string
		want    bool
	}{
		{"rank lists command", admin, cmdLatex, true},
		{"rank lacks command", user, cmdLatex, false},
		{"rank lists help", user, cmdHelp, true},
		{"unregistered user", unknown, cmdHelp, false},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			got, err := service.CheckPermission(t.Context(), scenario.jid, scenario.command)
			if err != nil {
				t.Fatal(err)
			}

			if got != scenario.want {
				t.Fatalf("CheckPermission(%q, %q) = %v, want %v", scenario.jid, scenario.command, got, scenario.want)
			}
		})
	}
}

func TestCheckPermissionRejectsInvalidCommand(t *testing.T) {
	t.Parallel()

	service, _ := newService(t)

	for _, command := range []string{"", "no spaces", "semi;colon"} {
		_, err := service.CheckPermission(t.Context(), "15550000001@s.whatsapp.net", command)
		if !errors.Is(err, auth.ErrInvalidInput) {
			t.Errorf("CheckPermission(%q) error = %v, want ErrInvalidInput", command, err)
		}
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

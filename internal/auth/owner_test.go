package auth_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
)

func TestParseOwners(t *testing.T) {
	t.Parallel()

	const (
		first  = "15551234567@s.whatsapp.net"
		second = "15557654321@s.whatsapp.net"
	)

	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty", "", nil},
		{"blank", " , ,", nil},
		{"single", first, []string{first}},
		{"several with spaces", " " + first + " , " + second + " ", []string{first, second}},
		{"skips blank entries", first + ",,  ,", []string{first}},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			got, err := auth.ParseOwners(scenario.raw)
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(got, scenario.want) {
				t.Fatalf("got %v, want %v", got, scenario.want)
			}
		})
	}
}

func TestParseOwnersRejectsMalformed(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"not-a-jid",
		"@s.whatsapp.net",
		"15551234567@",
		"15551234567@s.whatsapp.net@extra",
		"15551234567 @s.whatsapp.net",
		"15551234567:12@s.whatsapp.net",
		"15551234567.1@s.whatsapp.net",
		"15551234567.1:2@s.whatsapp.net",
		"15551234567@s.whatsapp.net,oops",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			_, err := auth.ParseOwners(raw)
			if !errors.Is(err, auth.ErrInvalidOwnerJID) {
				t.Fatalf("got %v, want ErrInvalidOwnerJID", err)
			}
		})
	}
}

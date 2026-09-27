package config_test

import (
	"testing"

	"botex/pkg/config"
)

func TestParseOwnerJIDs_Empty(t *testing.T) {
	t.Parallel()

	owners, err := config.ParseOwnerJIDs("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(owners) != 0 {
		t.Fatalf("expected no owners, got %v", owners)
	}
}

func TestParseOwnerJIDs_Single(t *testing.T) {
	t.Parallel()

	owners, err := config.ParseOwnerJIDs("15551234567@s.whatsapp.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(owners) != 1 || owners[0] != "15551234567@s.whatsapp.net" {
		t.Fatalf("unexpected owners: %v", owners)
	}
}

func TestParseOwnerJIDs_Several(t *testing.T) {
	t.Parallel()

	owners, err := config.ParseOwnerJIDs(" 15551234567@s.whatsapp.net , 15557654321@s.whatsapp.net ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"15551234567@s.whatsapp.net", "15557654321@s.whatsapp.net"}
	if len(owners) != len(want) {
		t.Fatalf("unexpected owners: %v", owners)
	}

	for i, jid := range want {
		if owners[i] != jid {
			t.Fatalf("owner %d: got %q, want %q", i, owners[i], jid)
		}
	}
}

func TestParseOwnerJIDs_SkipsBlankEntries(t *testing.T) {
	t.Parallel()

	owners, err := config.ParseOwnerJIDs("15551234567@s.whatsapp.net,,  ,")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(owners) != 1 {
		t.Fatalf("unexpected owners: %v", owners)
	}
}

func TestParseOwnerJIDs_Malformed(t *testing.T) {
	t.Parallel()

	cases := []string{
		"not-a-jid",
		"@s.whatsapp.net",
		"15551234567@",
		"15551234567@s.whatsapp.net@extra",
		"15551234567 @s.whatsapp.net",
	}

	for _, raw := range cases {
		_, err := config.ParseOwnerJIDs(raw)
		if err == nil {
			t.Errorf("expected error for malformed JID %q", raw)
		}
	}
}

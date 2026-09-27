package config

import (
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

var ErrInvalidOwnerJID = errors.New("invalid owner JID")

// ParseOwnerJIDs parses a comma-separated list of WhatsApp JIDs, as used by
// BOTEX_OWNER_JIDS. Blank entries are skipped. Each remaining entry must be a
// well-formed JID (user@server, no whitespace); malformed entries are
// rejected with ErrInvalidOwnerJID rather than seeded.
func ParseOwnerJIDs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	owners := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		jid, err := validateOwnerJID(trimmed)
		if err != nil {
			return nil, err
		}

		owners = append(owners, jid)
	}

	return owners, nil
}

func validateOwnerJID(raw string) (string, error) {
	if strings.ContainsAny(raw, " \t\n\r") {
		return "", fmt.Errorf("%w: %q", ErrInvalidOwnerJID, raw)
	}

	if strings.Count(raw, "@") != 1 {
		return "", fmt.Errorf("%w: %q", ErrInvalidOwnerJID, raw)
	}

	jid, err := types.ParseJID(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrInvalidOwnerJID, raw, err)
	}

	if jid.User == "" || jid.Server == "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidOwnerJID, raw)
	}

	return jid.String(), nil
}

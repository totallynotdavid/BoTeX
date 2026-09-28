package auth

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidOwnerJID = errors.New("invalid owner JID")

// ParseOwners parses a comma-separated list of WhatsApp JIDs, as used by
// BOTEX_OWNER_JIDS. Blank entries are skipped. Each other entry must be
// user@server with no whitespace and no device part (":device" or ".device"
// after the user), which is how message senders are keyed. Anything else fails
// with ErrInvalidOwnerJID instead of seeding an owner that can never match.
func ParseOwners(raw string) ([]string, error) {
	var owners []string

	for part := range strings.SplitSeq(raw, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		user, server, ok := strings.Cut(entry, "@")
		if !ok || user == "" || server == "" ||
			strings.ContainsAny(entry, " \t\n\r:") || strings.Contains(user, ".") || strings.Contains(server, "@") {
			return nil, fmt.Errorf("%w: %q", ErrInvalidOwnerJID, entry)
		}

		owners = append(owners, entry)
	}

	return owners, nil
}

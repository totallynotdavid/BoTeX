package auth

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidJID = errors.New("invalid JID")

// ParseJID checks one WhatsApp JID and returns it trimmed. It must be
// user@server with no whitespace and no device part (":device" or ".device"
// after the user), which is how message senders are keyed. Anything else fails
// with ErrInvalidJID instead of naming a JID that can never match.
func ParseJID(raw string) (string, error) {
	entry := strings.TrimSpace(raw)

	user, server, ok := strings.Cut(entry, "@")
	if !ok || user == "" || server == "" ||
		strings.ContainsAny(entry, " \t\n\r:") || strings.Contains(user, ".") || strings.Contains(server, "@") {
		return "", fmt.Errorf("%w: %q", ErrInvalidJID, entry)
	}

	return entry, nil
}

// ParseJIDs parses a comma-separated list of WhatsApp JIDs, as used by
// BOTKIT_OWNER_JIDS and BOTKIT_ALLOW_ONLY. Blank entries are skipped. Each
// other entry must satisfy ParseJID.
func ParseJIDs(raw string) ([]string, error) {
	var jids []string

	for part := range strings.SplitSeq(raw, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}

		jid, err := ParseJID(part)
		if err != nil {
			return nil, err
		}

		jids = append(jids, jid)
	}

	return jids, nil
}

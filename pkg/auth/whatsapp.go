package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// parseJID parses a user or group ID into a proper WhatsApp JID format.
func parseJID(id string) (string, error) {
	if id == "" {
		return "", errors.New("ID cannot be empty")
	}

	// If it already looks like a JID, return as-is
	if strings.Contains(id, "@") {
		return id, nil
	}

	// For group IDs, they typically end with @g.us
	if strings.Contains(id, "-") {
		return id + "@g.us", nil
	}

	// For user IDs, they typically end with @s.whatsapp.net
	return id + "@s.whatsapp.net", nil
}

// MockWhatsAppClient is a mock implementation for testing.
type MockWhatsAppClient struct {
	connected bool
	admins    map[string][]string // groupID -> []userID
}

// NewMockWhatsAppClient creates a new mock WhatsApp client.
func NewMockWhatsAppClient(connected bool) *MockWhatsAppClient {
	return &MockWhatsAppClient{
		connected: connected,
		admins:    make(map[string][]string),
	}
}

// IsConnected returns whether the mock client is connected.
func (m *MockWhatsAppClient) IsConnected() bool {
	return m.connected
}

// IsGroupAdmin checks if a user is an admin in a group (mock implementation).
func (m *MockWhatsAppClient) IsGroupAdmin(ctx context.Context, userJID, groupJID string) (bool, error) {
	if !m.connected {
		return false, errors.New("client not connected")
	}

	// Parse JIDs to ensure proper format
	parsedUserJID, err := parseJID(userJID)
	if err != nil {
		return false, fmt.Errorf("invalid user JID: %w", err)
	}

	parsedGroupJID, err := parseJID(groupJID)
	if err != nil {
		return false, fmt.Errorf("invalid group JID: %w", err)
	}

	// Check if user is in the admins list for this group
	if admins, exists := m.admins[parsedGroupJID]; exists {
		for _, adminJID := range admins {
			if adminJID == parsedUserJID {
				return true, nil
			}
		}
	}

	return false, nil
}

// SetGroupAdmin sets a user as admin in a group (for testing).
func (m *MockWhatsAppClient) SetGroupAdmin(groupJID, userJID string, isAdmin bool) error {
	parsedUserJID, err := parseJID(userJID)
	if err != nil {
		return fmt.Errorf("invalid user JID: %w", err)
	}

	parsedGroupJID, err := parseJID(groupJID)
	if err != nil {
		return fmt.Errorf("invalid group JID: %w", err)
	}

	if m.admins[parsedGroupJID] == nil {
		m.admins[parsedGroupJID] = []string{}
	}

	if isAdmin {
		// Add admin if not already present
		for _, adminJID := range m.admins[parsedGroupJID] {
			if adminJID == parsedUserJID {
				return nil // Already admin
			}
		}

		m.admins[parsedGroupJID] = append(m.admins[parsedGroupJID], parsedUserJID)
	} else {
		// Remove admin
		var newAdmins []string

		for _, adminJID := range m.admins[parsedGroupJID] {
			if adminJID != parsedUserJID {
				newAdmins = append(newAdmins, adminJID)
			}
		}

		m.admins[parsedGroupJID] = newAdmins
	}

	return nil
}

// SetConnected sets the connection status (for testing).
func (m *MockWhatsAppClient) SetConnected(connected bool) {
	m.connected = connected
}

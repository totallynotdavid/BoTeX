package auth

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// WhatsmeowClientAdapter adapts whatsmeow.Client to implement the WhatsAppClient interface.
type WhatsmeowClientAdapter struct {
	client *whatsmeow.Client
}

// NewWhatsmeowClientAdapter creates a new adapter for whatsmeow.Client.
func NewWhatsmeowClientAdapter(client *whatsmeow.Client) WhatsAppClient {
	return &WhatsmeowClientAdapter{
		client: client,
	}
}

// IsConnected checks if the WhatsApp client is connected.
func (w *WhatsmeowClientAdapter) IsConnected() bool {
	if w.client == nil {
		return false
	}

	return w.client.IsConnected()
}

// IsGroupAdmin checks if a user is a WhatsApp group admin.
func (w *WhatsmeowClientAdapter) IsGroupAdmin(ctx context.Context, userJID, groupJID string) (bool, error) {
	if w.client == nil {
		return false, errors.New("WhatsApp client not available")
	}

	if !w.client.IsConnected() {
		return false, errors.New("WhatsApp client not connected")
	}

	// Parse JIDs
	userJIDParsed, err := types.ParseJID(userJID)
	if err != nil {
		return false, fmt.Errorf("invalid user JID format: %w", err)
	}

	groupJIDParsed, err := types.ParseJID(groupJID)
	if err != nil {
		return false, fmt.Errorf("invalid group JID format: %w", err)
	}

	// Get group info from WhatsApp
	groupInfo, err := w.client.GetGroupInfo(groupJIDParsed)
	if err != nil {
		return false, fmt.Errorf("failed to get group info: %w", err)
	}

	// Check if user is in the admin list
	for _, participant := range groupInfo.Participants {
		if participant.JID.User == userJIDParsed.User && participant.IsAdmin {
			return true, nil
		}
	}

	return false, nil
}

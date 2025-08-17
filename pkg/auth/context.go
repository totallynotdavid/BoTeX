package auth

import (
	"context"
	"strings"

	"botex/pkg/message"
)

// MessageContext contains unified context information for permission handling
type MessageContext struct {
	Message     *message.Message
	UserID      string
	GroupID     string
	Command     string
	Permissions *PermissionResult
}

// ContextBuilder interface for creating message contexts
type ContextBuilder interface {
	BuildContext(ctx context.Context, msg *message.Message) (*MessageContext, error)
}

// contextBuilder implements the ContextBuilder interface
type contextBuilder struct {
	authService AuthService
}

// NewContextBuilder creates a new context builder with the provided auth service
func NewContextBuilder(authService AuthService) ContextBuilder {
	return &contextBuilder{
		authService: authService,
	}
}

// BuildContext creates a MessageContext from a message, extracting user, group, and command information
func (cb *contextBuilder) BuildContext(ctx context.Context, msg *message.Message) (*MessageContext, error) {
	msgCtx := &MessageContext{
		Message: msg,
	}

	// Extract user ID from sender
	msgCtx.UserID = msg.Sender.String()

	// Extract group ID if this is a group message
	if msg.IsGroup {
		msgCtx.GroupID = msg.GroupID.String()
	}

	// Extract command from message text
	msgCtx.Command = cb.extractCommand(msg.GetText())

	// If we have a command and auth service, check permissions
	if msgCtx.Command != "" && cb.authService != nil {
		permissions, err := cb.authService.CheckPermission(ctx, msgCtx.UserID, msgCtx.GroupID, msgCtx.Command)
		if err != nil {
			// Don't fail context building on permission check errors
			// The permission result will be nil, indicating an error occurred
			msgCtx.Permissions = &PermissionResult{
				Allowed: false,
				Reason:  "permission check failed: " + err.Error(),
			}
		} else {
			msgCtx.Permissions = permissions
		}
	}

	return msgCtx, nil
}

// extractCommand extracts the command name from message text
// Commands are expected to start with "!" prefix
func (cb *contextBuilder) extractCommand(text string) string {
	if !strings.HasPrefix(text, "!") {
		return ""
	}

	// Remove the "!" prefix and get the first word
	parts := strings.Fields(text[1:])
	if len(parts) == 0 {
		return ""
	}

	return parts[0]
}

// IsCommand checks if the message context contains a valid command
func (mc *MessageContext) IsCommand() bool {
	return mc.Command != ""
}

// IsAllowed checks if the command is allowed based on permissions
func (mc *MessageContext) IsAllowed() bool {
	return mc.Permissions != nil && mc.Permissions.Allowed
}

// GetPermissionReason returns the reason for permission denial or approval
func (mc *MessageContext) GetPermissionReason() string {
	if mc.Permissions == nil {
		return "no permission check performed"
	}
	return mc.Permissions.Reason
}

// GetUserRank returns the user's rank if available
func (mc *MessageContext) GetUserRank() string {
	if mc.Permissions == nil {
		return ""
	}
	return mc.Permissions.UserRank
}

// IsWhatsAppAdmin returns whether the user is a WhatsApp admin in the group
func (mc *MessageContext) IsWhatsAppAdmin() bool {
	if mc.Permissions == nil {
		return false
	}
	return mc.Permissions.IsWhatsAppAdmin
}

// IsGroupMessage returns whether this is a group message
func (mc *MessageContext) IsGroupMessage() bool {
	return mc.Message.IsGroup
}

// GetCommandArgs returns the command arguments (everything after the command name)
func (mc *MessageContext) GetCommandArgs() string {
	text := mc.Message.GetText()
	if !strings.HasPrefix(text, "!") {
		return ""
	}

	parts := strings.Fields(text[1:])
	if len(parts) <= 1 {
		return ""
	}

	return strings.Join(parts[1:], " ")
}
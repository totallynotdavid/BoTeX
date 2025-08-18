package auth

import (
	"strings"
)

func ValidateUserID(userID string) error {
	if userID == "" {
		return ErrInvalidInput
	}

	if len(userID) > 255 {
		return ErrInvalidInput
	}

	if !strings.Contains(userID, "@") {
		return ErrInvalidInput
	}

	return nil
}

func ValidateGroupID(groupID string) error {
	if groupID == "" {
		return nil // todo: is empty group ID is valid for private chats?
	}

	if len(groupID) > 255 {
		return ErrInvalidInput
	}

	if !strings.Contains(groupID, "@g.us") {
		return ErrInvalidInput
	}

	return nil
}

func ValidateCommand(command string) error {
	if command == "" {
		return ErrInvalidInput
	}

	if len(command) > 50 {
		return ErrInvalidInput
	}

	for _, r := range command {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-') {
			return ErrInvalidInput
		}
	}

	return nil
}

func ValidateRankName(name string) error {
	if name == "" {
		return ErrInvalidInput
	}

	if len(name) > 50 {
		return ErrInvalidInput
	}

	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return ErrInvalidInput
		}
	}

	return nil
}

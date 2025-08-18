package auth

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

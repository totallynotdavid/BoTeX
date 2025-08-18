package auth

const (
	maxCommandLength  = 50
	maxRankNameLength = 50
)

func ValidateCommand(command string) error {
	err := validateCommandLength(command)
	if err != nil {
		return err
	}

	return validateCommandCharacters(command)
}

func ValidateRankName(name string) error {
	err := validateRankNameLength(name)
	if err != nil {
		return err
	}

	return validateRankNameCharacters(name)
}

// helpers to avoid a cyclomatic complexity in the main validation functions.
func validateCommandLength(command string) error {
	if command == "" {
		return ErrInvalidInput
	}

	if len(command) > maxCommandLength {
		return ErrInvalidInput
	}

	return nil
}

func validateCommandCharacters(command string) error {
	for _, r := range command {
		if !isValidCommandChar(r) {
			return ErrInvalidInput
		}
	}

	return nil
}

func validateRankNameLength(name string) error {
	if name == "" {
		return ErrInvalidInput
	}

	if len(name) > maxRankNameLength {
		return ErrInvalidInput
	}

	return nil
}

func validateRankNameCharacters(name string) error {
	for _, r := range name {
		if !isValidRankChar(r) {
			return ErrInvalidInput
		}
	}

	return nil
}

// utils for the helpers.
func isValidCommandChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' || r == '-'
}

func isValidRankChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '_'
}

package util

import (
	"os"
	"strings"
)

func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}

// ParseBool parses a string as a boolean value.
// Accepts "true", "1" (case-insensitive) as true, everything else as false.
func ParseBool(value string) bool {
	return strings.EqualFold(value, "true") || value == "1"
}

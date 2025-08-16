package logger

import "strings"

func isWhatsmeowLogger(name string) bool {
	lowerName := strings.ToLower(name)

	prefixes := []string{"client", "whatsmeow"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lowerName, prefix) {
			return true
		}
	}

	return false
}

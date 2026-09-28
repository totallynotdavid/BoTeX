package fsm

// Levenshtein reaches the unexported distance from the external tests.
func Levenshtein(a, b string) int { return levenshtein(a, b) }

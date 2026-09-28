package flow

import "strings"

// keyAndValue is the size of one pair handed to strings.NewReplacer.
const keyAndValue = 2

// render replaces every "{{key}}" in text with data[key] and trims the result.
// It substitutes in one pass, so a value that contains "{{other}}" is left as
// it is.
func render(text string, data map[string]string) string {
	pairs := make([]string, 0, keyAndValue*len(data))
	for key, value := range data {
		pairs = append(pairs, "{{"+key+"}}", value)
	}

	return strings.TrimSpace(strings.NewReplacer(pairs...).Replace(text))
}

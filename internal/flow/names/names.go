// Package names picks a usable first name out of what a user typed or what
// WhatsApp shows as their profile name.
package names

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	minNameLength = 3
	maxWords      = 4
	vowels        = "aeiouáéíóúàèìòùâêîôûãõäëïöü"
)

// nonLetter matches what is neither a Unicode letter nor a hyphen.
var nonLetter = regexp.MustCompile(`[^\p{L}-]+`)

// FirstName returns the first plausible name in fullName, in title case, or ""
// when there is none. A word is plausible when, stripped of everything but
// letters and hyphens and cut at its first hyphen, it has at least three
// letters and one vowel. More than four words is a sentence, not a name.
func FirstName(fullName string) string {
	words := strings.Fields(fullName)

	if len(words) > maxWords {
		return ""
	}

	for _, word := range words {
		word, _, _ = strings.Cut(nonLetter.ReplaceAllString(word, ""), "-")

		if len([]rune(word)) < minNameLength {
			continue
		}

		if !strings.ContainsFunc(word, isVowel) {
			continue
		}

		return title(word)
	}

	return ""
}

func isVowel(r rune) bool {
	return strings.ContainsRune(vowels, unicode.ToLower(r))
}

// title lowercases s, then upper-cases its first letter, so "JOSÉ" becomes
// "José".
func title(s string) string {
	runes := []rune(strings.ToLower(s))
	runes[0] = unicode.ToTitle(runes[0])

	return string(runes)
}

//nolint:goconst // The names repeat across cases; literals keep each case readable.
package names_test

import (
	"testing"

	"github.com/totallynotdavid/botkit/internal/flow/names"
)

func TestFirstName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"one word", "Marta", "Marta"},
		{"lowercase", "marta", "Marta"},
		{"mixed case", "mArTa", "Marta"},
		{"upper case accented", "JOSÉ", "José"},
		{"accented", "Nuño", "Nuño"},
		{"three letters is enough", "Eva", "Eva"},
		{"two words take the first", "Marta Quispe", "Marta"},
		{"four words are still a name", "Marta Elena F. Quispe", "Marta"},
		{"five words are a sentence", "one two three four five", ""},
		{"skips a word that is too short", "de la Cruz", "Cruz"},
		{"skips a word without a vowel", "Mrtn Lucas", "Lucas"},
		{"cuts at the first hyphen", "Jean-Luc", "Jean"},
		{"a hyphen leading a short word", "-ab Lucas", "Lucas"},
		{"emoji around the name", "🔥Rafa🔥", "Rafa"},
		{"symbols around the name", "꧁Elena꧂", "Elena"},
		{"quotes around the name", `"Noé"`, "Noé"},
		{"leading dot", ".lenin Vargas", "Lenin"},
		{"emoji after the name", "Elena 🌸🥑🪼", "Elena"},
		{"brackets and a variation selector", "<Lucas >✌️", "Lucas"},
		{"a tilde before mixed case", "~lUcAs", "Lucas"},
		{"a curly apostrophe after a short word", "AG’ Vargas", "Vargas"},
		{"a musical symbol and a heart", "𝄞 rita ♡", "Rita"},
		{"a star after a three letter name", "Ali★", "Ali"},
		{"a long name before an emoji", "Aliii🌟", "Aliii"},
		{"capitals inside tildes", " ~DMA~", "Dma"},
		{"a two letter accented name", "Pío", "Pío"},
		{"one letter among symbols", " A~~~🧿", ""},
		{"digits are dropped", "marta2019", "Marta"},
		{"digits leave no vowel", "M4rt4", ""},
		{"a short word with emoji", "Ro😊", ""},
		{"only punctuation", ";", ""},
		{"only a tilde", "~", ""},
		{"only a dot", ".", ""},
		{"empty", "", ""},
		{"blank", "   ", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := names.FirstName(test.input)
			if got != test.want {
				t.Errorf("FirstName(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

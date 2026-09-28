//nolint:goconst // The words of each case repeat; literals keep each case readable.
package fsm_test

import (
	"testing"

	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

func TestLevenshtein(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from, to string
		want     int
	}{
		{"", "", 0},
		{"same", "same", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"hora", "hola", 1},
		{"menú", "menu", 1},
		{"año", "ano", 1},
		{"flaw", "lawn", 2},
	}

	for _, test := range tests {
		if got := fsm.Levenshtein(test.from, test.to); got != test.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", test.from, test.to, got, test.want)
		}

		if got := fsm.Levenshtein(test.to, test.from); got != test.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", test.to, test.from, got, test.want)
		}
	}
}

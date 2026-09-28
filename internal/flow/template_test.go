//nolint:goconst // The texts repeat across cases; literals keep each case readable.
package flow_test

import (
	"testing"

	"github.com/totallynotdavid/botkit/internal/flow"
)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		data map[string]string
		want string
	}{
		{"one key", "Hola {{name}}", map[string]string{"name": "Marta"}, "Hola Marta"},
		{"a key used twice", "{{a}} y {{a}}", map[string]string{"a": "x"}, "x y x"},
		{"two keys", "{{a}}-{{b}}", map[string]string{"a": "1", "b": "2"}, "1-2"},
		{"a key with no data stays", "Hola {{name}}", map[string]string{"other": "x"}, "Hola {{name}}"},
		{"no data", "Hola {{name}}", nil, "Hola {{name}}"},
		{"an empty value removes the key", "Hola {{name}}!", map[string]string{"name": ""}, "Hola !"},
		{"keys that share a prefix", "{{a}} {{ab}}", map[string]string{"a": "1", "ab": "2"}, "1 2"},
		{"the result is trimmed", "\n  {{a}}  \n", map[string]string{"a": "x"}, "x"},
		{"a value is not substituted again", "{{a}}", map[string]string{"a": "{{b}}", "b": "no"}, "{{b}}"},
		{"single braces stay", "{a} {{a}", map[string]string{"a": "x"}, "{a} {{a}"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := flow.Render(test.text, test.data)
			if got != test.want {
				t.Errorf("Render(%q, %v) = %q, want %q", test.text, test.data, got, test.want)
			}
		})
	}
}

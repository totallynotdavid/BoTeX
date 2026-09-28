//nolint:goconst // The locations repeat across cases; literals keep each case readable.
package fsm_test

import (
	"slices"
	"testing"

	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

func TestActionsListsEachPlaceOnce(t *testing.T) {
	t.Parallel()

	const data = `{
		"start_node": "A",
		"global_transitions": [
			{"condition": {"type": "exact", "value": ["ayuda"]}, "target": "NEEDS_ASSISTANCE", "action": "g"},
			{"condition": {"type": "exact", "value": ["fin"]}, "target": "NEEDS_ASSISTANCE"}
		],
		"transition_groups": {
			"menu": [{"condition": {"type": "any_text"}, "target": "B", "action": "m"}]
		},
		"nodes": {
			"B": {"message": {"type": "text", "content": "x"}, "include_transitions": "menu", "action": "b"},
			"A": {
				"message": {"type": "text", "content": "x"},
				"include_transitions": "menu",
				"transitions": [
					{"condition": {"type": "any_text"}, "target": "B"},
					{"condition": {"type": "exact", "value": ["x"]}, "target": "B", "action": "t"}
				]
			},
			"NEEDS_ASSISTANCE": {"message": {"type": "text", "content": "x"}}
		}
	}`

	flow, err := fsm.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	want := []fsm.ActionUse{
		{Where: "global_transitions[0]", Action: "g"},
		{Where: `group "menu"[0]`, Action: "m"},
		{Where: `node "A" transitions[1]`, Action: "t"},
		{Where: `node "B"`, Action: "b"},
	}

	// The order is fixed, so an error that lists these reads the same every run.
	for range 5 {
		got := flow.Actions()
		if !slices.Equal(got, want) {
			t.Fatalf("Actions() = %+v, want %+v", got, want)
		}
	}
}

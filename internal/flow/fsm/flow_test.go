package fsm_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

// anyAction accepts every action name, for tests of what is not an action.
func anyAction(string) bool { return true }

func parse(data string) (*fsm.Flow, error) {
	return fsm.Parse([]byte(data), anyAction) //nolint:wrapcheck // the tests read Parse's error as it is.
}

// A mistyped action fails the load, and the error names every place it sits so
// the operator fixes the flow in one pass.
func TestParseReportsEveryUnknownAction(t *testing.T) {
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
			"B": {"message": {"content": "x"}, "include_transitions": "menu", "action": "b"},
			"A": {
				"message": {"content": "x"},
				"include_transitions": "menu",
				"transitions": [
					{"condition": {"type": "any_text"}, "target": "B"},
					{"condition": {"type": "exact", "value": ["x"]}, "target": "B", "action": "t"}
				]
			},
			"NEEDS_ASSISTANCE": {"message": {"content": "x"}}
		}
	}`

	known := func(action string) bool { return action == "t" }

	_, err := fsm.Parse([]byte(data), known)
	if !errors.Is(err, fsm.ErrAction) {
		t.Fatalf("Parse() = %v, want %v", err, fsm.ErrAction)
	}

	for _, where := range []string{
		`global_transitions[0]: unknown action: "g"`,
		`group "menu"[0]: unknown action: "m"`,
		`node "B": unknown action: "b"`,
	} {
		if !strings.Contains(err.Error(), where) {
			t.Errorf("Parse() error %q does not say %s", err, where)
		}
	}

	if strings.Contains(err.Error(), `"t"`) {
		t.Errorf("Parse() error %q names the known action \"t\"", err)
	}
}

// A node's own transitions are numbered as in the flow file, where the group
// the node includes is not part of the list, so the place an operator is told
// to look is the place Parse names for a mistake in the same transition.
func TestParseNumbersOwnTransitionsAsTheFileDoes(t *testing.T) {
	t.Parallel()

	const (
		group = `"transition_groups": {"menu": [
			{"condition": {"type": "any_text"}, "target": "B"},
			{"condition": {"type": "exact", "value": ["x"]}, "target": "B"}
		]},`
		node = `"nodes": {
			"A": {"message": {"content": "x"}, "include_transitions": "menu",
				"transitions": [{"condition": {"type": "media"}, "target": %q%s}]},
			"B": {"message": {"content": "x"}},
			"NEEDS_ASSISTANCE": {"message": {"content": "x"}}
		}`
		head  = `{"start_node": "A",`
		place = `node "A" transitions[0]`
	)

	_, err := fsm.Parse([]byte(head+group+fmt.Sprintf(node, "B", `, "action": "boom"`)+`}`), func(string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), place) {
		t.Errorf("Parse() error = %v, want it to name %s for the action", err, place)
	}

	_, err = parse(head + group + fmt.Sprintf(node, "NOWHERE", "") + `}`)
	if err == nil || !strings.Contains(err.Error(), place) {
		t.Errorf("Parse() error = %v, want it to name %s for the target", err, place)
	}
}

// A transition's react reaches the route of a global and of a node transition.
// A fallback carries none.
func TestRouteCarriesTheReactOfItsTransition(t *testing.T) {
	t.Parallel()

	flow, err := parse(`{"start_node":"A",
		"global_transitions":[{"condition":{"type":"exact","value":["gracias"]},"target":"B","react":"🙏"}],
		"nodes":{
			"A":{"message":{"content":"a"},"transitions":[{"condition":{"type":"exact","value":["ok"]},"target":"B","react":"✅"}]},
			"B":{"message":{"content":"b"}},
			"NEEDS_ASSISTANCE":{"message":{"content":"x"}}}}`)
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}

	for text, want := range map[string]string{"ok": "✅", "gracias": "🙏", "otra cosa": ""} {
		if got := flow.DetermineNext("A", text, "").React; got != want {
			t.Errorf("DetermineNext(A, %q).React = %q, want %q", text, got, want)
		}
	}
}

func TestParseChecksThatAReactionIsOneEmoji(t *testing.T) {
	t.Parallel()

	reacting := func(react string) string {
		quoted, err := json.Marshal(react)
		if err != nil {
			t.Fatal(err)
		}

		return `{"start_node":"A","nodes":{
			"A":{"message":{"content":"a"},"react":` + string(quoted) + `},
			"NEEDS_ASSISTANCE":{"message":{"content":"x"}}}}`
	}

	valid := []string{
		"✅", "👍", "❤️", "🙏", "👍🏽", "🇵🇪", "1️⃣", "#️⃣", "👨‍👩‍👧‍👦", "🏴󠁧󠁢󠁥󠁮󠁧󠁿", "⭐",
	}
	for _, react := range valid {
		_, err := parse(reacting(react))
		if err != nil {
			t.Errorf("Parse() with react %q = %v, want it to load", react, err)
		}
	}

	joiner := string(rune(0x200d))

	invalid := []string{
		joiner, "👍" + joiner, joiner + "👍",
		"👍 ok", "ok", "thanks!", ":check:", "✅✅✅", "✅👍", "a", "1", "👍 ", " 👍", "🏽", "🇵",
	}
	for _, react := range invalid {
		_, err := parse(reacting(react))
		if !errors.Is(err, fsm.ErrReact) || !strings.Contains(err.Error(), `node "A"`) {
			t.Errorf("Parse() with react %q = %v, want %v naming node \"A\"", react, err, fsm.ErrReact)
		}
	}
}

func TestParseRejectsTheRemovedMessageType(t *testing.T) {
	t.Parallel()

	_, err := parse(`{"start_node":"A","nodes":{"A":{"message":{"type":"text","content":"a"}}}}`)
	if !errors.Is(err, fsm.ErrParse) || !strings.Contains(err.Error(), `"type"`) {
		t.Errorf("Parse() = %v, want %v naming the field \"type\"", err, fsm.ErrParse)
	}
}

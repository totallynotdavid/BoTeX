//nolint:goconst // The messages and JSON of each case repeat; literals keep each case readable.
package fsm_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

func TestParseRejects(t *testing.T) {
	t.Parallel()

	const (
		toB       = `{"condition":{"type":"any_text"},"target":"B"}`
		toNowhere = `{"condition":{"type":"any_text"},"target":"NOWHERE"}`
	)

	tests := []struct {
		name string
		json string
		want error
		// in is what the message must name, so an operator finds the mistake.
		in []string
	}{
		{"empty start node", `{"nodes":{"NEEDS_ASSISTANCE":` + stub + `}}`, fsm.ErrStartNode, []string{`start_node ""`}},
		{"missing start node", `{"start_node":"X","nodes":{"NEEDS_ASSISTANCE":` + stub + `}}`, fsm.ErrStartNode, []string{`start_node "X"`}},
		{"missing help node", `{"start_node":"A","nodes":{"A":` + stub + `}}`, fsm.ErrHelpNode, []string{fsm.HelpNode}},
		{"node transition to a missing node", flowJSON("", toB+","+toNowhere), fsm.ErrTarget, []string{`node "A" transitions[1]`, `"NOWHERE"`}},
		{"global transition to a missing node", flowJSON(`"global_transitions":[`+toB+`,`+toNowhere+`],`, ""), fsm.ErrTarget, []string{"global_transitions[1]", `"NOWHERE"`}},
		{"group transition to a missing node", flowJSON(`"transition_groups":{"g":[`+toNowhere+`]},`, ""), fsm.ErrTarget, []string{`group "g"[0]`, `"NOWHERE"`}},
		{"unknown condition type", flowJSON("", toB+`,{"condition":{"type":"fuzzy"},"target":"B"}`), fsm.ErrCondition, []string{`node "A" transitions[1]`, `"fuzzy"`}},
		{"regex that does not compile", flowJSON("", condition(`{"type":"regex","regex":"(unclosed"}`)), fsm.ErrRegex, []string{`node "A" transitions[0]`, "(unclosed"}},
		{"regex without a pattern", flowJSON("", condition(`{"type":"regex"}`)), fsm.ErrValues, []string{`node "A" transitions[0]`, "regex"}},
		{"exact without values", flowJSON("", condition(`{"type":"exact"}`)), fsm.ErrValues, []string{`node "A" transitions[0]`}},
		{"keyword without values", flowJSON("", condition(`{"type":"keyword","value":[]}`)), fsm.ErrValues, []string{`node "A" transitions[0]`}},
		{"media_type without values", flowJSON("", condition(`{"type":"media_type"}`)), fsm.ErrValues, []string{`node "A" transitions[0]`}},
		{"blank keyword", flowJSON("", condition(`{"type":"keyword","value":["ok"," "]}`)), fsm.ErrValues, []string{`node "A" transitions[0]`, "blank"}},
		{"unknown media kind", flowJSON("", condition(`{"type":"media_type","value":["imagen"]}`)), fsm.ErrMediaKind, []string{`node "A" transitions[0]`, `"imagen"`}},
		{"a field the flow does not know", `{"start_node":"A","nodes":{"A":{"ignore_global_transition":true}}}`, fsm.ErrParse, []string{"ignore_global_transition"}},
		{"invalid JSON", `{"start_node":`, fsm.ErrParse, nil},
		{"data after the flow", flowJSON("", toB) + `{}`, fsm.ErrParse, nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			flow, err := fsm.Parse([]byte(test.json))
			if !errors.Is(err, test.want) {
				t.Fatalf("Parse() = %v, %v, want an error wrapping %q", flow, err, test.want)
			}

			for _, s := range test.in {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("Parse() error %q does not name %s", err, s)
				}
			}
		})
	}
}

func TestParseRejectsIncludeOfMissingGroup(t *testing.T) {
	t.Parallel()

	const nodes = `"A":{"message":{"type":"text","content":"a"},"include_transitions":"g"},"NEEDS_ASSISTANCE":` + stub

	for name, top := range map[string]string{
		"no groups at all":      "",
		"only other groups":     `"transition_groups":{"other":[]},`,
		"a group of another id": `"transition_groups":{"G":[]},`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := fsm.Parse([]byte(`{"start_node":"A",` + top + `"nodes":{` + nodes + `}}`))
			if !errors.Is(err, fsm.ErrInclude) || !strings.Contains(err.Error(), `node "A"`) || !strings.Contains(err.Error(), `"g"`) {
				t.Errorf("Parse() = %v, want %v naming node \"A\" and group \"g\"", err, fsm.ErrInclude)
			}
		})
	}
}

func TestParseReportsEveryMistake(t *testing.T) {
	t.Parallel()

	flow := `{"start_node":"X","nodes":{
		"A":{"message":{"type":"text","content":"a"},"transitions":[
			{"condition":{"type":"nope"},"target":"B"},
			{"condition":{"type":"regex","regex":"("},"target":"NOWHERE"}]},
		"NEEDS_ASSISTANCE":` + stub + `}}`

	_, err := fsm.Parse([]byte(flow))
	for _, want := range []error{fsm.ErrStartNode, fsm.ErrCondition, fsm.ErrRegex, fsm.ErrTarget} {
		if !errors.Is(err, want) {
			t.Errorf("Parse() = %v, want it to include %q", err, want)
		}
	}
}

// Nodes that include one group must each get the group followed by their own
// transitions, however the transition slices happen to be allocated.
func TestParseMergesGroupsPerNode(t *testing.T) {
	t.Parallel()

	targets := func(node fsm.Node) []string {
		out := make([]string, 0, len(node.Transitions))
		for _, tr := range node.Transitions {
			out = append(out, tr.Target)
		}

		return out
	}

	flow, err := fsm.Parse([]byte(`{"start_node":"A",
		"transition_groups":{"g":[
			{"condition":{"type":"exact","value":["1"]},"target":"G1"},
			{"condition":{"type":"exact","value":["2"]},"target":"G2"},
			{"condition":{"type":"exact","value":["3"]},"target":"G3"}]},
		"nodes":{
			"A":{"message":{"type":"text","content":"a"},"include_transitions":"g","transitions":[{"condition":{"type":"any_text"},"target":"OWN_A"}]},
			"B":{"message":{"type":"text","content":"b"},"include_transitions":"g","transitions":[{"condition":{"type":"any_text"},"target":"OWN_B"}]},
			"C":{"message":{"type":"text","content":"c"},"include_transitions":"g"},
			"G1":` + stub + `,"G2":` + stub + `,"G3":` + stub + `,"OWN_A":` + stub + `,"OWN_B":` + stub + `,"NEEDS_ASSISTANCE":` + stub + `}}`))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}

	for node, want := range map[string]string{"A": "G1 G2 G3 OWN_A", "B": "G1 G2 G3 OWN_B", "C": "G1 G2 G3"} {
		if got := strings.Join(targets(flow.Nodes[node]), " "); got != want {
			t.Errorf("node %s routes to %q, want %q", node, got, want)
		}
	}

	if got := flow.DetermineNext("B", "anything", "").Node; got != "OWN_B" {
		t.Errorf("DetermineNext(B) = %q, want OWN_B", got)
	}
}

func TestExampleParses(t *testing.T) {
	t.Parallel()

	flow, err := fsm.Example()
	if err != nil {
		t.Fatalf("Example() = %v", err)
	}

	if flow.StartNode != "GREETING_INTRO" {
		t.Errorf("start node = %q, want GREETING_INTRO", flow.StartNode)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	custom := filepath.Join(dir, "custom.json")
	invalid := filepath.Join(dir, "invalid.json")

	err := os.WriteFile(custom, []byte(flowJSON("", "")), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(invalid, []byte(`{"start_node":"MISSING","nodes":{}}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{"a valid file", custom, nil},
		{"a missing file", filepath.Join(dir, "absent.json"), fs.ErrNotExist},
		{"an invalid file", invalid, fsm.ErrStartNode},
		{"a path that cannot be read", dir, syscall.EISDIR},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			flow, err := fsm.Load(test.path)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Load(%q) = %v, want %v", test.path, err, test.wantErr)
			}

			if err == nil && flow.StartNode != "A" {
				t.Errorf("Load(%q) starts at %q, want A", test.path, flow.StartNode)
			}
		})
	}
}

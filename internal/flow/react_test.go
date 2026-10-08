package flow_test

import "testing"

// The flow file decides when the bot reacts: the react of the transition taken,
// else the react of the node entered, and never after a fallback.
func TestFlowFileDecidesTheReaction(t *testing.T) {
	t.Parallel()

	parsed := tiny(t, `
		"START":{"message":{"content":"start"},"transitions":[
			{"condition":{"type":"exact","value":["transition"]},"target":"PLAIN","react":"👍"},
			{"condition":{"type":"exact","value":["both"]},"target":"WARM","react":"✅"},
			{"condition":{"type":"exact","value":["node"]},"target":"WARM"},
			{"condition":{"type":"exact","value":["none"]},"target":"PLAIN"}]},
		"PLAIN":{"message":{"content":"plain"}},
		"WARM":{"message":{"content":"warm"},"react":"🎉"}`)

	tests := []struct {
		name, text, want string
	}{
		{"the transition's react", "transition", "👍"},
		{"the transition's react beats the node's", "both", "✅"},
		{"the node's react when the transition has none", "node", "🎉"},
		{"no react anywhere", "none", ""},
		{"a fallback never reacts", "unmatched", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			env := start(t, withFlow(parsed))
			env.seed(t, "START")

			err := env.say(t, test.text)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			got := env.client.Reactions()
			if test.want == "" {
				if len(got) != 0 {
					t.Errorf("reactions = %+v, want none", got)
				}

				return
			}

			if len(got) != 1 || got[0].Emoji != test.want {
				t.Errorf("reactions = %+v, want one %s", got, test.want)
			}
		})
	}
}

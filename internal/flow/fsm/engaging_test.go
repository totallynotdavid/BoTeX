//nolint:wsl_v5 // Route tables and flow assertions read as compact transcripts.
package fsm_test

import (
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

const (
	fallback   = fsm.ActionFallbackResponse
	wrongMedia = fsm.ActionFallbackWrongMedia
	aboutNode  = "ABOUT"
	offNode    = "FOLLOW_UP_OFF"
	identity   = "quien eres"
)

type route struct {
	name       string
	node       string
	text       string
	media      bot.MediaKind
	wantNode   string
	wantAction string
}

func text(name, node, input, wantNode, wantAction string) route {
	return route{name: name, node: node, text: input, wantNode: wantNode, wantAction: wantAction}
}

func file(name, node string, media bot.MediaKind, caption, wantNode, wantAction string) route {
	return route{name: name, node: node, text: caption, media: media, wantNode: wantNode, wantAction: wantAction}
}

func runRoutes(t *testing.T, flow *fsm.Flow, routes []route) {
	t.Helper()
	for _, test := range routes {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := flow.DetermineNext(test.node, test.text, test.media)
			if got.Node != test.wantNode || got.Action != test.wantAction {
				t.Errorf("DetermineNext(%q, %q, %q) = (%q, %q), want (%q, %q)", test.node, test.text, test.media, got.Node, got.Action, test.wantNode, test.wantAction)
			}
		})
	}
}

func TestEngagingFlowRoutesGlobalQuestionsBeforeLocalKeywords(t *testing.T) {
	t.Parallel()

	flow, err := fsm.EngagingExample(anyAction)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name, node, text, want string
	}{
		{"identity from mystery", "MYSTERY", identity, aboutNode},
		{"identity from poetry", "POETRY", identity, aboutNode},
		{"identity from beginner", "BEGINNER", identity, aboutNode},
		{"help from mystery", "MYSTERY", "ayuda", fsm.HelpNode},
		{"opt out from memory", "MEMORY", "no mas recordatorios", offNode},
		{"opt out from about", aboutNode, "no mas recordatorios", offNode},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := flow.DetermineNext(test.node, test.text, "")
			if got.Node != test.want || got.Via != fsm.ViaGlobal {
				t.Fatalf("DetermineNext(%q, %q) = (%q, %q), want global %q", test.node, test.text, got.Node, got.Via, test.want)
			}
		})
	}
}

func TestEngagingFlowHasRecoveryForFollowUpStates(t *testing.T) {
	t.Parallel()

	flow, err := fsm.EngagingExample(anyAction)
	if err != nil {
		t.Fatal(err)
	}

	for _, node := range []string{"FOLLOW_UP_ON", "FOLLOW_UP_OFF"} {
		route := flow.DetermineNext(node, "algo que no entiendo", "")
		if route.Node != node || route.Via != fsm.ViaFallback {
			t.Errorf("fallback from %s = (%q, %q), want to stay in the node", node, route.Node, route.Via)
		}
		if flow.Nodes[node].FallbackMessage == "" {
			t.Errorf("node %s has no fallback message", node)
		}
	}
}

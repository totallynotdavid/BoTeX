//nolint:goconst // Node IDs repeat across cases; literals keep each case readable against example.json.
package fsm_test

import (
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

const (
	fallback   = fsm.ActionFallbackResponse
	wrongMedia = fsm.ActionFallbackWrongMedia
)

type route struct {
	name       string
	node       string
	text       string
	media      bot.MediaKind
	wantNode   string
	wantAction string
}

func text(name, node, text, wantNode, wantAction string) route {
	return route{name: name, node: node, text: text, wantNode: wantNode, wantAction: wantAction}
}

func file(name, node string, media bot.MediaKind, caption, wantNode, wantAction string) route {
	return route{name: name, node: node, text: caption, media: media, wantNode: wantNode, wantAction: wantAction}
}

func exampleFlow(t *testing.T) *fsm.Flow {
	t.Helper()

	flow, err := fsm.Example()
	if err != nil {
		t.Fatalf("Example() = %v", err)
	}

	return flow
}

func runRoutes(t *testing.T, flow *fsm.Flow, routes []route) {
	t.Helper()

	for _, rte := range routes {
		t.Run(rte.name, func(t *testing.T) {
			t.Parallel()

			got := flow.DetermineNext(rte.node, rte.text, rte.media)
			if got.Node != rte.wantNode || got.Action != rte.wantAction {
				t.Errorf("DetermineNext(%q, %q, %q) = (%q, %q), want (%q, %q)",
					rte.node, rte.text, rte.media, got.Node, got.Action, rte.wantNode, rte.wantAction)
			}
		})
	}
}

func TestDetermineNextExample(t *testing.T) {
	t.Parallel()

	flow := exampleFlow(t)

	t.Run("start menu", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			text("digit 1", "GREETING_INTRO", "1", "INTERESTED_IN_BEGINNER", ""),
			text("digit 2", "GREETING_INTRO", "2", "INTERESTED_IN_ADVANCED_CATEGORIES", ""),
			text("keyword with a typo", "GREETING_INTRO", "principiantee", "INTERESTED_IN_BEGINNER", ""),
			text("trimmed and lowercased", "GREETING_INTRO", "  PRINCIPIANTE \n", "INTERESTED_IN_BEGINNER", ""),
			text("digit as a word of a sentence", "GREETING_INTRO", "opción 1 por favor", "INTERESTED_IN_BEGINNER", ""),
			text("digit inside a longer number", "GREETING_INTRO", "tengo 21 años", "GREETING_INTRO", fallback),
		})
	})

	t.Run("main menu", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			text("prices", "MAIN_MENU", "precios", "CONSULTED_PRICE", ""),
			text("prices with a typo", "MAIN_MENU", "presio", "CONSULTED_PRICE", ""),
			text("schedule", "MAIN_MENU", "horarios", "CONSULTED_SCHEDULE", ""),
			text("hora is a schedule", "MAIN_MENU", "a qué hora empezamos", "CONSULTED_SCHEDULE", ""),
			text("hola is not hora", "MAIN_MENU", "hola", "MAIN_MENU", fallback),
			text("enrollment", "MAIN_MENU", "quiero inscribirme", "CLARIFY_ENROLLMENT_COURSE", ""),
			text("first listed transition wins", "MAIN_MENU", "cuanto cuesta la matricula", "CONSULTED_PRICE", ""),
			text("exact does not match a longer text", "INTERESTED_IN_ADVANCED_CATEGORIES", "10", "INTERESTED_IN_ADVANCED_CATEGORIES", fallback),
			text("gibberish", "MAIN_MENU", "zzzzzz", "MAIN_MENU", fallback),
			text("menu keyword with its accent", "CONSULTED_PRICE", "menú", "MAIN_MENU", ""),
			text("global help", "MAIN_MENU", "necesito ayuda", "NEEDS_ASSISTANCE", ""),
			text("global goodbye", "MAIN_MENU", "adiós", "CONVERSATION_CLOSED", ""),
		})
	})

	t.Run("names", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			text("clear the name", "MAIN_MENU", "quita mi nombre", "NAME_REMOVED_CONFIRMATION", "clear_user_name"),
			text("set the name by regex", "MAIN_MENU", "Me llamo Carla", "NAME_CHANGE_CONFIRMED", "save_user_name"),
			text("the regex needs a name after it", "MAIN_MENU", "me llamo", "MAIN_MENU", fallback),
			text("change the name", "MAIN_MENU", "cambiar nombre", "CHANGE_NAME_PROMPT", ""),
			text("skipped on a node that ignores globals", "CLUB_MISTERIO", "cambiar nombre", "CLUB_MISTERIO", fallback),
			text("any text is the new name, even a help word", "CHANGE_NAME_PROMPT", "ayuda", "NAME_CHANGE_CONFIRMED", "save_user_name"),
			text("blank text is not any_text", "CHANGE_NAME_PROMPT", "   ", "CHANGE_NAME_PROMPT", fallback),
			file("a caption is not any_text", "CHANGE_NAME_PROMPT", bot.MediaImage, "Ana", "CHANGE_NAME_PROMPT", fallback),
			text("the name confirmation shares the main menu", "NAME_CHANGE_CONFIRMED", "precio", "CONSULTED_PRICE", ""),
		})
	})

	t.Run("clubs", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			text("category by digit", "INTERESTED_IN_ADVANCED_CATEGORIES", "1", "ADVANCED_MENU_FICTION", ""),
			text("category by word", "INTERESTED_IN_ADVANCED_CATEGORIES", "novela", "ADVANCED_MENU_FICTION", ""),
			text("catalog", "INTERESTED_IN_ADVANCED_CATEGORIES", "catálogo", "CONSULTED_CATALOG", ""),
			text("club from a category menu", "ADVANCED_MENU_FICTION", "misterio", "CLUB_MISTERIO", ""),
			text("back from a category menu", "ADVANCED_MENU_FICTION", "volver", "INTERESTED_IN_ADVANCED_CATEGORIES", ""),
			text("club from the catalog", "CONSULTED_CATALOG", "poesía", "CLUB_POESIA", ""),
			text("club from the group of a second node", "PROMPT_ADVANCED_COURSE_FOR_ENROLLMENT", "historia", "CLUB_HISTORICA", ""),
			text("enroll in a club", "CLUB_MISTERIO", "quiero inscribirme", "CONFIRM_ENROLLMENT", "set_selected_course"),
			text("navigation works while globals are ignored", "CLUB_MISTERIO", "menu", "MAIN_MENU", ""),
			text("help works while globals are ignored", "CLUB_MISTERIO", "ayuda", "NEEDS_ASSISTANCE", ""),
			text("goodbye is ignored", "CLUB_MISTERIO", "adios", "CLUB_MISTERIO", fallback),
			text("leaving a node does not report its action", "INTERESTED_IN_BEGINNER", "precio", "CONSULTED_PRICE", ""),
			text("beginner enrollment", "INTERESTED_IN_BEGINNER", "inscribirme", "CONFIRM_ENROLLMENT_BEGINNER", ""),
		})
	})

	t.Run("enrollment", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			text("confirm", "CONFIRM_ENROLLMENT", "si", "ENROLLMENT_PROCESS", ""),
			text("confirm with punctuation", "CONFIRM_ENROLLMENT", "¡Sí, claro!", "ENROLLMENT_PROCESS", ""),
			text("cancel", "CONFIRM_ENROLLMENT", "cancelar", "ENROLLMENT_CANCELLED", ""),
			text("si is not found inside asi", "CONFIRM_ENROLLMENT", "asi no quiero", "ENROLLMENT_CANCELLED", ""),
			text("no is not found inside conozco", "CONFIRM_ENROLLMENT", "ya conozco el curso", "CONFIRM_ENROLLMENT", fallback),
			text("goodbye is allowed here", "CONFIRM_ENROLLMENT", "adios", "CONVERSATION_CLOSED", ""),
			text("the group is shared by the beginner node", "CONFIRM_ENROLLMENT_BEGINNER", "sí", "ENROLLMENT_PROCESS", ""),
			text("cancelled goes back to the menu options", "ENROLLMENT_CANCELLED", "1", "INTERESTED_IN_BEGINNER", ""),
			text("clarify beginner", "CLARIFY_ENROLLMENT_COURSE", "1", "CONFIRM_ENROLLMENT_BEGINNER", ""),
			text("clarify by word", "CLARIFY_ENROLLMENT_COURSE", "nuevos", "CONFIRM_ENROLLMENT_BEGINNER", ""),
			text("clarify advanced", "CLARIFY_ENROLLMENT_COURSE", "2", "PROMPT_ADVANCED_COURSE_FOR_ENROLLMENT", ""),
		})
	})

	t.Run("payment", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			text("paid without a voucher", "ENROLLMENT_PROCESS", "listo", "WAITING_FOR_VOUCHER", ""),
			file("voucher image", "ENROLLMENT_PROCESS", bot.MediaImage, "", "PAYMENT_CONFIRMED", "save_payment_voucher"),
			file("media rules beat a caption keyword", "ENROLLMENT_PROCESS", bot.MediaImage, "listo", "PAYMENT_CONFIRMED", "save_payment_voucher"),
			file("media rules beat a cancelling caption", "ENROLLMENT_PROCESS", bot.MediaImage, "volver", "PAYMENT_CONFIRMED", "save_payment_voucher"),
			file("voucher image while waiting", "WAITING_FOR_VOUCHER", bot.MediaImage, "", "PAYMENT_CONFIRMED", "save_payment_voucher"),
			text("own help keyword", "ENROLLMENT_PROCESS", "ayuda", "NEEDS_ASSISTANCE", ""),
			text("cancel while waiting", "WAITING_FOR_VOUCHER", "cancelar", "ENROLLMENT_CANCELLED", ""),
			text("goodbye is ignored", "ENROLLMENT_PROCESS", "adios", "ENROLLMENT_PROCESS", fallback),
			text("the help override is allowed", "ENROLLMENT_PROCESS", "quiero hablar con una asesora", "NEEDS_ASSISTANCE", ""),
			file("wrong media kind", "ENROLLMENT_PROCESS", bot.MediaVideo, "", "ENROLLMENT_PROCESS", wrongMedia),
			file("wrong media kind, caption matches the node", "ENROLLMENT_PROCESS", bot.MediaVideo, "ayuda", "NEEDS_ASSISTANCE", ""),
			file("wrong media kind, caption matches the help global", "WAITING_FOR_VOUCHER", bot.MediaDocument, "necesito una asesora", "NEEDS_ASSISTANCE", ""),
			file("second image", "PAYMENT_CONFIRMED", bot.MediaImage, "", "PAYMENT_CONFIRMED_ACK_EXTRA", "save_payment_voucher"),
			file("third image", "PAYMENT_CONFIRMED_ACK_EXTRA", bot.MediaImage, "", "PAYMENT_CONFIRMED_ACK_EXTRA", "save_payment_voucher"),
			text("text after payment", "PAYMENT_CONFIRMED", "gracias", "PAYMENT_CONFIRMED", fallback),
			text("menu after payment", "PAYMENT_CONFIRMED_ACK_EXTRA", "menu", "MAIN_MENU", ""),
		})
	})

	t.Run("assistance and closing", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			text("urgent", "NEEDS_ASSISTANCE", "urgente", "URGENT_ASSISTANCE", ""),
			text("help again stays in the help node", "NEEDS_ASSISTANCE", "ayuda", "NEEDS_ASSISTANCE", ""),
			file("any media in the help node", "NEEDS_ASSISTANCE", bot.MediaAudio, "", "ASSISTANCE_ATTACHMENT", ""),
			text("a terminal node falls back", "URGENT_ASSISTANCE", "gracias", "URGENT_ASSISTANCE", fallback),
			text("a terminal node still honors globals", "URGENT_ASSISTANCE", "menu", "MAIN_MENU", ""),
			text("any text reopens the menu", "CONVERSATION_CLOSED", "hola de nuevo", "MAIN_MENU", ""),
			file("a caption does not reopen it", "CONVERSATION_CLOSED", bot.MediaImage, "hola", "CONVERSATION_CLOSED", fallback),
		})
	})

	t.Run("fallbacks", func(t *testing.T) {
		t.Parallel()

		runRoutes(t, flow, []route{
			file("media in a node without media rules", "MAIN_MENU", bot.MediaAudio, "", "MAIN_MENU", fallback),
			file("an image in a node without media rules", "MAIN_MENU", bot.MediaImage, "", "MAIN_MENU", fallback),
			file("a global keyword in a caption", "MAIN_MENU", bot.MediaImage, "ayuda", "NEEDS_ASSISTANCE", ""),
			file("a video where an image is expected", "WAITING_FOR_VOUCHER", bot.MediaVideo, "", "WAITING_FOR_VOUCHER", wrongMedia),
			file("a sticker where an image is expected", "PAYMENT_CONFIRMED", bot.MediaSticker, "", "PAYMENT_CONFIRMED", wrongMedia),
			text("unknown node restarts", "GHOST", "precio", "GREETING_INTRO", ""),
		})
	})
}

func TestDetermineNextVia(t *testing.T) {
	t.Parallel()

	flow := exampleFlow(t)

	tests := []struct {
		name  string
		node  string
		text  string
		media bot.MediaKind
		want  fsm.Via
	}{
		{"node transition", "MAIN_MENU", "precios", "", fsm.ViaNode},
		{"media transition", "ENROLLMENT_PROCESS", "", bot.MediaImage, fsm.ViaNode},
		{"global transition", "MAIN_MENU", "adios", "", fsm.ViaGlobal},
		{"help global on a node that ignores globals", "CLUB_MISTERIO", "ayuda", "", fsm.ViaGlobal},
		{"generic fallback", "MAIN_MENU", "zzzz", "", fsm.ViaFallback},
		{"wrong media fallback", "ENROLLMENT_PROCESS", "", bot.MediaVideo, fsm.ViaFallback},
		{"unknown node", "GHOST", "precios", "", fsm.ViaReset},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := flow.DetermineNext(test.node, test.text, test.media).Via; got != test.want {
				t.Errorf("DetermineNext(%q, %q, %q).Via = %q, want %q", test.node, test.text, test.media, got, test.want)
			}
		})
	}
}

func TestDetermineNextConcurrent(t *testing.T) {
	t.Parallel()

	flow := exampleFlow(t)

	// One route per kind of condition that matches at runtime: fuzzy keyword, regex, media.
	routes := []route{
		text("", "GREETING_INTRO", "principiantee", "INTERESTED_IN_BEGINNER", ""),
		text("", "MAIN_MENU", "Me llamo Carla", "NAME_CHANGE_CONFIRMED", "save_user_name"),
		file("", "ENROLLMENT_PROCESS", bot.MediaImage, "listo", "PAYMENT_CONFIRMED", "save_payment_voucher"),
	}

	var workers sync.WaitGroup

	for range 16 {
		workers.Go(func() {
			for range 200 {
				for _, want := range routes {
					got := flow.DetermineNext(want.node, want.text, want.media)
					if got.Node != want.wantNode || got.Action != want.wantAction {
						t.Errorf("DetermineNext(%q, %q, %q) = (%q, %q), want (%q, %q)",
							want.node, want.text, want.media, got.Node, got.Action, want.wantNode, want.wantAction)
					}
				}
			}
		})
	}

	workers.Wait()
}

// featuresOf names what a flow uses: "condition <type>", "action <name>" and
// the structural features the example must show.
func featuresOf(flow *fsm.Flow) map[string]bool {
	features := map[string]bool{}
	transitions := slices.Clone(flow.GlobalTransitions)

	for group := range maps.Values(flow.TransitionGroups) {
		transitions = append(transitions, group...)
	}

	includes := map[string]int{}

	for id := range flow.Nodes {
		node := flow.Nodes[id]
		transitions = append(transitions, node.Transitions...)
		features["action "+node.Action] = true
		features["a node that ignores globals"] = features["a node that ignores globals"] || node.IgnoreGlobalTransitions
		features["a node with a fallback message"] = features["a node with a fallback message"] || node.FallbackMessage != ""
		includes[node.IncludeTransitions]++
	}

	for _, tr := range transitions {
		features["condition "+string(tr.Condition.Type)] = true
		features["action "+tr.Action] = true
	}

	delete(includes, "")

	features["a group included by two nodes"] = slices.Max(append(slices.Collect(maps.Values(includes)), 0)) >= 2
	features["global transitions"] = len(flow.GlobalTransitions) > 0

	return features
}

// The example flow is what F2 tests its actions against, so it must keep using
// every feature and every action the runtime knows.
func TestExampleCoversTheFeatures(t *testing.T) {
	t.Parallel()

	features := featuresOf(exampleFlow(t))

	for _, want := range []string{
		"condition exact", "condition keyword", "condition regex", "condition any_text", "condition media", "condition media_type",
		"action clear_user_name", "action create_new_lead", "action escalate_to_human_agent", "action save_payment_voucher",
		"action save_user_name", "action set_selected_course", "action update_lead_consulted_price",
		"action update_lead_interest_advanced", "action update_lead_interest_beginner",
		"global transitions", "a node that ignores globals", "a group included by two nodes", "a node with a fallback message",
	} {
		if !features[want] {
			t.Errorf("the example flow has no %s", want)
		}
	}
}

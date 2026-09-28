//nolint:goconst // Node names repeat across cases; literals keep each case readable against its flow.
package flow_test

import (
	"fmt"
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

const (
	wrongMediaFormat = "Parece que enviaste un tipo de archivo incorrecto. Por favor, asegúrate de enviar %s para que pueda procesarlo. Gracias 😊"

	askingForFiles = `
		"START":{"message":{"type":"text","content":"start"}},
		"WANTS_VIDEO":{"message":{"type":"text","content":"send a video"},
			"transitions":[{"condition":{"type":"media_type","value":["video"]},"target":"DONE"}]},
		"WANTS_ANY":{"message":{"type":"text","content":"send anything"},
			"transitions":[{"condition":{"type":"media"},"target":"DONE"}]},
		"DONE":{"message":{"type":"text","content":"thanks"}}`
)

func TestNodeTakesTheMediaItAsksFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node string
		kind bot.MediaKind
	}{
		{"the kind of media_type", "WANTS_VIDEO", bot.MediaVideo},
		{"an audio for media", "WANTS_ANY", bot.MediaAudio},
		{"a sticker for media", "WANTS_ANY", bot.MediaSticker},
		{"a video for media", "WANTS_ANY", bot.MediaVideo},
		{"a document for media", "WANTS_ANY", bot.MediaDocument},
		{"an image for media", "WANTS_ANY", bot.MediaImage},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			env := start(t, withFlow(tiny(t, askingForFiles)))
			env.seed(t, test.node)

			err := env.sendMedia(t, test.kind, []byte("data"))
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(), "thanks")
			requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "DONE")
		})
	}
}

func TestWrongMediaReplyNamesEveryKindTheNodeAsksFor(t *testing.T) {
	t.Parallel()

	nodes := `
		"START":{"message":{"type":"text","content":"start"}},
		"WANTS_DOCUMENT":{"message":{"type":"text","content":"send a file"},
			"transitions":[{"condition":{"type":"media_type","value":["document"]},"target":"START"}]},
		"WANTS_EITHER":{"message":{"type":"text","content":"send a video or a file"},
			"transitions":[{"condition":{"type":"media_type","value":["video","document"]},"target":"START"}]},
		"WANTS_TWICE":{"message":{"type":"text","content":"send a video"},
			"transitions":[{"condition":{"type":"media_type","value":["video"]},"target":"START"},
				{"condition":{"type":"media_type","value":["video","audio"]},"target":"START"}]},
		"WANTS_AUDIO":{"message":{"type":"text","content":"send a voice note"},
			"transitions":[{"condition":{"type":"media_type","value":["audio"]},"target":"START"}]},
		"WANTS_STICKER":{"message":{"type":"text","content":"send a sticker"},
			"transitions":[{"condition":{"type":"media_type","value":["sticker"]},"target":"START"}]}`

	tests := map[string]string{
		"WANTS_DOCUMENT": "un *documento*",
		"WANTS_EITHER":   "un *video* o un *documento*",
		"WANTS_TWICE":    "un *video* o un *audio*",
		"WANTS_AUDIO":    "un *audio*",
		"WANTS_STICKER":  "un *sticker*",
	}

	for node, asked := range tests {
		t.Run(node, func(t *testing.T) {
			t.Parallel()

			env := start(t, withFlow(tiny(t, nodes)))
			env.seed(t, node)

			// An image is the one kind of file that reaches a node that asks for others.
			err := env.sendMedia(t, bot.MediaImage, []byte("data"))
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(), fmt.Sprintf(wrongMediaFormat, asked))
		})
	}
}

// Wrong files count like wrong text: the third hands the user to a person.
func TestThirdWrongFileEscalates(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(tiny(t, askingForFiles)))
	env.seed(t, "WANTS_VIDEO")

	for count := 1; count <= 2; count++ {
		err := env.sendMedia(t, bot.MediaImage, []byte("data"))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}

		state := env.state(t)
		requireEqual(t, "CurrentNode", state.CurrentNode, "WANTS_VIDEO")
		requireEqual(t, "RepromptCount", state.RepromptCount, count)
		requireEqual(t, "RequiresHumanAgent", state.RequiresHumanAgent, false)
	}

	err := env.sendMedia(t, bot.MediaImage, []byte("data"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	state := env.state(t)
	requireEqual(t, "CurrentNode", state.CurrentNode, "NEEDS_ASSISTANCE")
	requireEqual(t, "RequiresHumanAgent", state.RequiresHumanAgent, true)
	requireEqual(t, "RepromptCount", state.RepromptCount, 0)

	sent := env.sent()
	requireEqual(t, "replies", len(sent), 3)
	requireEqual(t, "last reply", sent[2], "a person will help")
}

func TestUnsupportedMediaOnANodeTheFlowNoLongerHas(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.seed(t, "REMOVED_NODE")

	err := env.sendMedia(t, bot.MediaSticker, []byte("data"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent(), unsupportedMedia)
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "REMOVED_NODE")

	// Text sends the user to the start.
	err = env.say(t, "hola")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent(), env.text("GREETING_INTRO", welcome))
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "GREETING_INTRO")
}

func TestCourseNameFallsBackWhenTheSelectedNodeHasNoTitle(t *testing.T) {
	t.Parallel()

	nodes := `
		"START":{"message":{"type":"text","content":"start"}},
		"UNTITLED":{"message":{"type":"text","content":"an untitled node"}},
		"CONFIRM":{"message":{"type":"text","content":"Enroll in {{course_name}}?"},
			"transitions":[{"condition":{"type":"exact","value":["yes"]},"target":"START"}]}`

	for _, selected := range []string{"UNTITLED", "REMOVED_NODE"} {
		t.Run(selected, func(t *testing.T) {
			t.Parallel()

			env := start(t, withFlow(tiny(t, nodes)))
			env.seed(t, "CONFIRM", func(state *flow.State) { state.SelectedCourseID = selected })

			err := env.say(t, "zzzz")
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(),
				"No entendí tu respuesta 😊 Por favor, revisa las opciones:\n\nEnroll in el curso seleccionado?")
		})
	}
}

// A node that includes a group has a way out even when the group is empty, so
// it is not the end of a conversation.
func TestNodeThatIncludesAGroupIsNotTheEndOfTheConversation(t *testing.T) {
	t.Parallel()

	parsed, err := fsm.Parse([]byte(`{"start_node":"START","transition_groups":{"shared":[]},"nodes":{
		"START":{"message":{"type":"text","content":"start"}},
		"HUB":{"message":{"type":"text","content":"pick one"},"include_transitions":"shared"},
		"NEEDS_ASSISTANCE":{"message":{"type":"text","content":"a person will help"}}}}`))
	if err != nil {
		t.Fatal(err)
	}

	env := start(t, withFlow(parsed))
	env.seed(t, "HUB")

	err = env.say(t, "zzzz")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent(), "No entendí tu respuesta 😊 Por favor, revisa las opciones:\n\npick one")
}

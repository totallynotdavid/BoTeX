//nolint:goconst // The JSON of a condition repeats across cases; literals keep each case readable.
package fsm_test

import (
	"fmt"
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

// stub is a node with no transitions.
const stub = `{"message":{"type":"text","content":"x"}}`

// flowJSON is a flow whose start node A holds the given transitions, with the
// nodes every flow needs. top is JSON members for the flow's top level.
func flowJSON(top, transitions string) string {
	return fmt.Sprintf(`{"start_node":"A",%s"nodes":{
		"A":{"message":{"type":"text","content":"a"},"transitions":[%s]},
		"B":%[3]s,"NEEDS_ASSISTANCE":%[3]s}}`, top, transitions, stub)
}

func condition(cond string) string {
	return `{"condition":` + cond + `,"target":"B"}`
}

func TestConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cond  string
		text  string
		media bot.MediaKind
		want  bool
	}{
		{"exact: one of several values", `{"type":"exact","value":["1","uno"]}`, "uno", "", true},
		{"exact: case is ignored", `{"type":"exact","value":["Sí"]}`, "SÍ", "", true},
		{"exact: the text is trimmed", `{"type":"exact","value":["1"]}`, "  1\n", "", true},
		{"exact: not a substring", `{"type":"exact","value":["1"]}`, "10", "", false},
		{"exact: not a prefix", `{"type":"exact","value":["uno"]}`, "uno mas", "", false},
		{"exact: accents are significant", `{"type":"exact","value":["basico"]}`, "básico", "", false},
		{"exact: a caption is compared like text", `{"type":"exact","value":["listo"]}`, "Listo", bot.MediaImage, true},

		{"keyword: word of a sentence", `{"type":"keyword","value":["help"]}`, "i need help please", "", true},
		{"keyword: followed by punctuation", `{"type":"keyword","value":["help"]}`, "please, HELP!", "", true},
		{"keyword: accents are ignored", `{"type":"keyword","value":["menu"]}`, "volver al menú", "", true},
		{"keyword: accents are ignored in the keyword", `{"type":"keyword","value":["menú"]}`, "menu", "", true},
		{"keyword: the keyword's case is ignored", `{"type":"keyword","value":["HELP"]}`, "help", "", true},
		{"keyword: second keyword", `{"type":"keyword","value":["nothing","help"]}`, "help", "", true},
		{"keyword: several words in a row", `{"type":"keyword","value":["hasta luego"]}`, "bueno, hasta luego amigo", "", true},
		{"keyword: several words apart", `{"type":"keyword","value":["hasta luego"]}`, "hasta pronto luego", "", false},
		{"keyword: no keyword present", `{"type":"keyword","value":["help"]}`, "all fine", "", false},
		{"keyword: empty text", `{"type":"keyword","value":["x"]}`, "", "", false},
		{"keyword: not inside a longer word", `{"type":"keyword","value":["si"]}`, "presion", "", false},
		{"keyword: not at the end of a longer word", `{"type":"keyword","value":["no"]}`, "conozco el curso", "", false},
		{"keyword: a digit is a word", `{"type":"keyword","value":["1"]}`, "opción 1", "", true},
		{"keyword: a digit is not part of a number", `{"type":"keyword","value":["1"]}`, "21", "", false},
		{"keyword: symbols compare the raw text", `{"type":"keyword","value":["?"]}`, "¿qué?", "", true},
		{"keyword: a typo in a long word", `{"type":"keyword","value":["precio"]}`, "presio", "", true},
		{"keyword: two edits", `{"type":"keyword","value":["biblioteca"]}`, "bibliotekaa", "", true},
		{"keyword: three edits", `{"type":"keyword","value":["biblioteca"]}`, "bibliotekaas", "", false},
		{"keyword: hora is not hola", `{"type":"keyword","value":["hora"]}`, "hola", "", false},
		{"keyword: hola is not hora", `{"type":"keyword","value":["hola"]}`, "hora", "", false},
		{"keyword: a short word matches only itself", `{"type":"keyword","value":["hora"]}`, "hora", "", true},
		{"keyword: a long word does not typo-match a short one", `{"type":"keyword","value":["precio"]}`, "pre", "", false},
		{"keyword: a phrase must match exactly", `{"type":"keyword","value":["cambiar nombre"]}`, "cambiar nombrr", "", false},
		{"keyword: two edits in a long word", `{"type":"keyword","value":["precio"]}`, "presiu", "", true},
		{"keyword: three edits in a long word", `{"type":"keyword","value":["precio"]}`, "presuu", "", false},
		{"keyword: only the misspelled word is compared", `{"type":"keyword","value":["precio"]}`, "cual es el presio", "", true},
		{"keyword: an accent is one edit", `{"type":"keyword","value":["atrás"]}`, "atras", "", true},
		{"keyword: two edits in a five letter word", `{"type":"keyword","value":["costo"]}`, "cxxto", "", true},
		{"keyword: three edits in a five letter word", `{"type":"keyword","value":["costo"]}`, "cxxxo", "", false},
		{"keyword: a short word against a long keyword", `{"type":"keyword","value":["costo"]}`, "cost", "", false},
		{"keyword: two deletions against a long keyword", `{"type":"keyword","value":["costo"]}`, "cos", "", false},
		{"keyword: a short keyword and a longer word", `{"type":"keyword","value":["gato"]}`, "gatxo", "", false},
		{"keyword: a short keyword and two extra letters", `{"type":"keyword","value":["gato"]}`, "gatxx", "", false},
		{"keyword: two letters never match loosely", `{"type":"keyword","value":["ok"]}`, "oik", "", false},
		{"keyword: a one letter keyword", `{"type":"keyword","value":["1"]}`, "l", "", false},
		{"keyword: letters are counted, not bytes, in a short word", `{"type":"keyword","value":["ñoña"]}`, "ñoño", "", false},
		{"keyword: letters are counted, not bytes, in a two letter word", `{"type":"keyword","value":["ñóx"]}`, "ñó", "", false},
		{"keyword: a caption is searched", `{"type":"keyword","value":["ayuda"]}`, "necesito ayuda", bot.MediaImage, true},

		{"regex: matches inside the text", `{"type":"regex","regex":"\\d{3}"}`, "code 123 ok", "", true},
		{"regex: the pattern's case is ignored", `{"type":"regex","regex":"ABC"}`, "xabcx", "", true},
		{"regex: anchors hold", `{"type":"regex","regex":"^abc$"}`, "abcd", "", false},
		{"regex: no match", `{"type":"regex","regex":"^\\d+$"}`, "12a", "", false},
		{"regex: an alternation", `{"type":"regex","regex":"ord(en|er)"}`, "mi orden llegó", "", true},
		{"regex: the text is lowercased before matching", `{"type":"regex","regex":"^Hola$"}`, "HOLA", "", true},
		{"regex: a caption is matched", `{"type":"regex","regex":"^\\d+$"}`, "42", bot.MediaImage, true},
		{"regex: the value list is ignored", `{"type":"regex","regex":"chau","value":["hola"]}`, "hola", "", false},

		{"any_text: text", `{"type":"any_text"}`, "hello", "", true},
		{"any_text: blank text", `{"type":"any_text"}`, "  ", "", false},
		{"any_text: a caption is not text", `{"type":"any_text"}`, "hello", bot.MediaImage, false},

		{"media: an image", `{"type":"media"}`, "", bot.MediaImage, true},
		{"media: a sticker", `{"type":"media"}`, "", bot.MediaSticker, true},
		{"media: text", `{"type":"media"}`, "hello", "", false},

		{"media_type: the kind", `{"type":"media_type","value":["image"]}`, "", bot.MediaImage, true},
		{"media_type: one of several", `{"type":"media_type","value":["image","video"]}`, "", bot.MediaVideo, true},
		{"media_type: another kind", `{"type":"media_type","value":["image"]}`, "", bot.MediaVideo, false},
		{"media_type: text", `{"type":"media_type","value":["image"]}`, "image", "", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			flow, err := fsm.Parse([]byte(flowJSON("", condition(test.cond))))
			if err != nil {
				t.Fatalf("Parse() = %v", err)
			}

			if got := flow.DetermineNext("A", test.text, test.media).Node == "B"; got != test.want {
				t.Errorf("%s matched %q with media %q = %v, want %v", test.cond, test.text, test.media, got, test.want)
			}
		})
	}
}

func TestRoutingOrder(t *testing.T) {
	t.Parallel()

	transitions := `
		{"condition":{"type":"keyword","value":["caption"]},"target":"TEXT"},
		{"condition":{"type":"keyword","value":["both"]},"target":"TEXT_FIRST","action":"caption_action"},
		{"condition":{"type":"media_type","value":["image"]},"target":"IMAGE"}`
	nodes := `"TEXT":` + stub + `,"TEXT_FIRST":` + stub + `,"IMAGE":` + stub + `,"ANY":` + stub + `,"GLOBAL":` + stub

	flow, err := fsm.Parse([]byte(`{"start_node":"A",
		"global_transitions":[{"condition":{"type":"keyword","value":["both","global"]},"target":"GLOBAL","action":"global_action"}],
		"nodes":{
			"A":{"message":{"type":"text","content":"a"},"transitions":[` + transitions + `]},
			"M":{"message":{"type":"text","content":"m"},"transitions":[{"condition":{"type":"media"},"target":"ANY"}]},
			` + nodes + `,"NEEDS_ASSISTANCE":` + stub + `}}`))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}

	tests := []route{
		text("the global transition wins before local order", "A", "both", "GLOBAL", "global_action"),
		text("a global transition beats a local one", "A", "caption both", "GLOBAL", "global_action"),
		text("a global applies when the node has no match", "A", "global", "GLOBAL", "global_action"),
		text("media conditions never match text", "A", "image", "A", fsm.ActionFallbackResponse),
		file("media is tried before an earlier caption transition", "A", bot.MediaImage, "caption", "IMAGE", ""),
		file("a caption transition still applies to other media", "A", bot.MediaAudio, "caption", "TEXT", ""),
		file("the global transition wins for a media caption", "A", bot.MediaAudio, "both", "GLOBAL", "global_action"),
		file("the media condition takes any file", "M", bot.MediaSticker, "", "ANY", ""),
	}

	runRoutes(t, flow, tests)
}

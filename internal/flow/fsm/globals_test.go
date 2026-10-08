package fsm_test

import (
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

// This focused flow has one global list and a node that ignores it, so the
// rules between globals, node transitions and media get their own flow.
func TestGlobalsAndMedia(t *testing.T) {
	t.Parallel()

	const (
		toLocal    = `{"condition":{"type":"keyword","value":["local"]},"target":"LOCAL"}`
		wantsVideo = `{"condition":{"type":"media_type","value":["video"]},"target":"DONE"}`
	)

	flow, err := fsm.Parse([]byte(`{"start_node":"OPEN",
		"global_transitions":[
			{"condition":{"type":"keyword","value":["both"]},"target":"GLOBAL","action":"first_global"},
			{"condition":{"type":"keyword","value":["both"]},"target":"LOCAL","action":"second_global"},
			{"condition":{"type":"keyword","value":["help"]},"target":"NEEDS_ASSISTANCE","action":"help_action"},
			{"condition":{"type":"keyword","value":["local"]},"target":"GLOBAL"},
			{"condition":{"type":"keyword","value":["menu"]},"target":"DONE"}],
		"nodes":{
			"OPEN":{"message":{"type":"text","content":"o"},"transitions":[` + toLocal + `]},
			"CLOSED":{"message":{"type":"text","content":"c"},"ignore_global_transitions":true,"transitions":[` + toLocal + `]},
			"WANTS_VIDEO":{"message":{"type":"text","content":"v"},"transitions":[` + wantsVideo + `]},
			"LOCAL":` + stub + `,"GLOBAL":` + stub + `,"DONE":` + stub + `,"NEEDS_ASSISTANCE":` + stub + `}}`))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}

	runRoutes(t, flow, []route{
		text("a global transition beats a node transition", "OPEN", "local", "GLOBAL", ""),
		text("the first global wins", "OPEN", "both", "GLOBAL", "first_global"),
		text("the help global on an open node", "OPEN", "help", "NEEDS_ASSISTANCE", "help_action"),
		text("a closed node skips the other globals", "CLOSED", "both", "CLOSED", fallback),
		text("a closed node still takes the help global", "CLOSED", "help", "NEEDS_ASSISTANCE", "help_action"),
		text("a closed node keeps its own transitions", "CLOSED", "local", "LOCAL", ""),
		file("globals apply to captions", "OPEN", bot.MediaImage, "both", "GLOBAL", "first_global"),
		file("a closed node ignores globals in captions", "CLOSED", bot.MediaImage, "both", "CLOSED", fallback),
		file("the wrong kind of media", "WANTS_VIDEO", bot.MediaImage, "", "WANTS_VIDEO", wrongMedia),
		file("a global in the caption beats the wrong media fallback", "WANTS_VIDEO", bot.MediaImage, "menu", "DONE", ""),
		text("text is never the wrong media", "WANTS_VIDEO", "hello", "WANTS_VIDEO", fallback),
		file("the media kind it asks for", "WANTS_VIDEO", bot.MediaVideo, "", "DONE", ""),
		file("media in a node without media rules", "OPEN", bot.MediaImage, "", "OPEN", fallback),
	})
}

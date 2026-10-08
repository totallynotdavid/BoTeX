package main_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	flowcmd "github.com/totallynotdavid/botkit/cmd/flow"
	"github.com/totallynotdavid/botkit/internal/cli"
)

func TestEngagingREPLRunsAConversationOverTheFakeTransport(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := cli.RunREPL(t.Context(), flowcmd.Command(), environment(t, map[string]string{}), slog.New(slog.DiscardHandler), strings.NewReader("hola\nmisteryo\nqué elegí\n:quit\n"), &out)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}

	transcript := out.String()
	for _, want := range []string{"Soy *Luma*", "Club Misterio", "Recuerdo tu última elección"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("REPL transcript = %q, want %q", transcript, want)
		}
	}
}

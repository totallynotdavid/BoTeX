//nolint:goconst // The words of the command line repeat across cases; literals keep each case readable.
package main_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flowcmd "github.com/totallynotdavid/botkit/cmd/flow"
	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/sqlite"
)

// handoff runs the flow binary's handoff command against the store at path.
func handoff(t *testing.T, path string, want int, args ...string) string {
	t.Helper()

	t.Setenv(config.KeyStore, path)

	var out bytes.Buffer

	if got := cli.Execute(t.Context(), flowcmd.Command(), append([]string{"handoff"}, args...), &out); got != want {
		t.Errorf("flow handoff %s exited %d, want %d", strings.Join(args, " "), got, want)
	}

	return out.String()
}

func TestHandoffListsAndClearsTheUsersWaitingForAPerson(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	vars := map[string]string{}

	// "ayuda" sends Alice to the node that hands her to a person. Bob only greets.
	converse(t, vars, []bot.Message{{Sender: alice, Text: "ayuda"}, {Sender: bob, Text: "buenas"}}, 2)

	path := vars[config.KeyStore]

	list := handoff(t, path, cli.ExitOK, "list")
	if !strings.Contains(list, string(alice)) || !strings.Contains(list, "NEEDS_ASSISTANCE") || !strings.Contains(list, "ayuda") {
		t.Errorf("handoff list = %q, want Alice, her node and her last message", list)
	}

	if strings.Contains(list, string(bob)) {
		t.Errorf("handoff list = %q, want no row for Bob", list)
	}

	cleared := handoff(t, path, cli.ExitOK, "clear", string(alice))
	if !strings.Contains(cleared, string(alice)) {
		t.Errorf("handoff clear wrote %q, want Alice's JID", cleared)
	}

	list = handoff(t, path, cli.ExitOK, "list")
	if strings.Contains(list, string(alice)) {
		t.Errorf("handoff list = %q after clear, want no row for Alice", list)
	}

	handoff(t, path, cli.ExitFailure, "clear", string(alice))
	handoff(t, path, cli.ExitFailure, "clear", string(bob))
}

// A store the bot has not created is a wrong directory or environment. The
// commands say so and create nothing, rather than report an empty queue.
func TestHandoffFailsWhenTheStoreDoesNotExist(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	path := filepath.Join(t.TempDir(), "botkit.db")

	for _, args := range [][]string{{"list"}, {"clear", string(alice)}} {
		out := handoff(t, path, cli.ExitFailure, args...)
		if out != "" {
			t.Errorf("flow handoff %s wrote %q, want no output", strings.Join(args, " "), out)
		}
	}

	_, err := os.Stat(path)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("flow handoff created the store: Stat() = %v", err)
	}
}

func TestHandoffRefusesBadArguments(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	path := filepath.Join(t.TempDir(), "bot.db")

	database, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	err = database.Close()
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{}, {"list", "extra"}, {"clear"}, {"clear", "not-a-jid"}, {"drop", string(alice)}} {
		handoff(t, path, cli.ExitUsage, args...)
	}
}

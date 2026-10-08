//nolint:goconst // The words of the command line repeat across cases; literals keep each case readable.
package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/sqlite"
)

// ranked is a command with two ranks and groups: the first rank is what a
// user gets without --rank.
func ranked() cli.Command {
	return cli.Command{
		Name:   "ranked",
		Groups: true,
		Ranks: []auth.Rank{
			{Name: "member", Level: 1, Commands: []string{"help"}},
			{Name: "admin", Level: 2, Commands: []string{"*"}},
		},
		Configure: idle().Configure,
	}
}

// operate runs the command line args against the store in the environment and
// returns the exit status and what the command wrote to its output.
func operate(t *testing.T, cmd cli.Command, args ...string) (status int, out string) {
	t.Helper()

	var written bytes.Buffer

	status = cli.Execute(t.Context(), cmd, args, &written)

	return status, written.String()
}

// useStore points the commands at a store the bot has created, as its first
// start does.
func useStore(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bot.db")

	database, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	err = database.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.KeyStore, path)
}

// A command run from the wrong directory or without the bot's environment
// finds no store. It fails and creates nothing, so the operator is never told
// a user was added to a database the bot does not read.
func TestSubcommandsFailWhenTheStoreDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.KeyStore, filepath.Join(dir, "botkit.db"))

	cmd := ranked()

	for _, args := range [][]string{
		{"user", "add", "51900000001@s.whatsapp.net"},
		{"user", "list"},
		{"group", "add", "120363000000000000@g.us"},
	} {
		out := run(t, cmd, cli.ExitFailure, args...)
		if out != "" {
			t.Errorf("%s wrote %q, want no output", strings.Join(args, " "), out)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) > 0 {
		t.Errorf("the commands created %d files in the store's directory, want none", len(entries))
	}
}

func requireStatus(t *testing.T, args []string, got, want int) {
	t.Helper()

	if got != want {
		t.Errorf("%s exited %d, want %d", strings.Join(args, " "), got, want)
	}
}

func run(t *testing.T, cmd cli.Command, want int, args ...string) string {
	t.Helper()

	status, out := operate(t, cmd, args...)
	requireStatus(t, args, status, want)

	return out
}

func TestUserCommandsManageTheRegisteredUsers(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	useStore(t)

	cmd := ranked()

	out := run(t, cmd, cli.ExitOK, "user", "add", "51900000001@s.whatsapp.net")
	if !strings.Contains(out, "51900000001@s.whatsapp.net as member") {
		t.Errorf("user add wrote %q, want the JID and the default rank", out)
	}

	run(t, cmd, cli.ExitOK, "user", "add", "51900000002@s.whatsapp.net", "--rank", "admin")
	run(t, cmd, cli.ExitOK, "user", "add", "--rank", "admin", "51900000003@s.whatsapp.net")

	list := run(t, cmd, cli.ExitOK, "user", "list")
	for _, want := range []string{"51900000001@s.whatsapp.net  member", "51900000002@s.whatsapp.net  admin", "51900000003@s.whatsapp.net  admin"} {
		if !strings.Contains(list, want) {
			t.Errorf("user list = %q, want a row %q", list, want)
		}
	}

	run(t, cmd, cli.ExitFailure, "user", "add", "51900000001@s.whatsapp.net")
	run(t, cmd, cli.ExitFailure, "user", "add", "51900000009@s.whatsapp.net", "--rank", "ghost")

	run(t, cmd, cli.ExitOK, "user", "remove", "51900000001@s.whatsapp.net")
	run(t, cmd, cli.ExitFailure, "user", "remove", "51900000001@s.whatsapp.net")

	list = run(t, cmd, cli.ExitOK, "user", "list")
	if strings.Contains(list, "51900000001") {
		t.Errorf("user list = %q after remove, want the user gone", list)
	}

	// Removing then adding is how the rank of a user changes.
	run(t, cmd, cli.ExitOK, "user", "add", "51900000001@s.whatsapp.net", "--rank", "admin")

	list = run(t, cmd, cli.ExitOK, "user", "list")
	if !strings.Contains(list, "51900000001@s.whatsapp.net  admin") {
		t.Errorf("user list = %q, want the user back as admin", list)
	}
}

func TestUserCommandsRefuseBadArguments(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	useStore(t)

	cmd := ranked()

	for _, args := range [][]string{
		{"user"},
		{"user", "frobnicate"},
		{"user", "add"},
		{"user", "add", "not-a-jid"},
		{"user", "add", "51900000001"},
		{"user", "add", "51900000001@s.whatsapp.net", "surplus"},
		{"user", "add", "51900000001@s.whatsapp.net", "--nope"},
		{"user", "remove"},
		{"user", "list", "surplus"},
	} {
		run(t, cmd, cli.ExitUsage, args...)
	}
}

func TestGroupCommandsManageTheRegisteredGroups(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	useStore(t)

	cmd := ranked()

	run(t, cmd, cli.ExitOK, "group", "add", "120363000000000000@g.us")
	run(t, cmd, cli.ExitFailure, "group", "add", "120363000000000000@g.us")
	run(t, cmd, cli.ExitUsage, "group", "add", "51900000001@s.whatsapp.net")
	run(t, cmd, cli.ExitUsage, "group", "add", "nonsense")

	list := run(t, cmd, cli.ExitOK, "group", "list")
	if !strings.Contains(list, "120363000000000000@g.us") {
		t.Errorf("group list = %q, want the group", list)
	}

	run(t, cmd, cli.ExitOK, "group", "remove", "120363000000000000@g.us")
	run(t, cmd, cli.ExitFailure, "group", "remove", "120363000000000000@g.us")

	list = run(t, cmd, cli.ExitOK, "group", "list")
	if strings.Contains(list, "120363") {
		t.Errorf("group list = %q after remove, want it empty", list)
	}

	// A removed group registers again.
	run(t, cmd, cli.ExitOK, "group", "add", "120363000000000000@g.us")
}

// The user and group commands exist for a bot that has ranks and groups.
func TestCommandLineHasOnlyTheSubcommandsOfTheBot(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	useStore(t)

	run(t, idle(), cli.ExitUsage, "user", "list")
	run(t, idle(), cli.ExitUsage, "group", "list")

	noGroups := ranked()
	noGroups.Groups = false

	run(t, noGroups, cli.ExitOK, "user", "list")
	run(t, noGroups, cli.ExitUsage, "group", "list")
}

func TestBotSubcommandRunsAgainstTheOpenStore(t *testing.T) { //nolint:paralleltest // The store path comes from the environment.
	useStore(t)

	cmd := idle()
	cmd.Subcommands = []cli.Subcommand{{
		Name:  "count",
		Usage: "[--help]",
		Run: func(ctx context.Context, operator cli.Operator, args []string) error {
			if len(args) > 0 {
				return cli.ErrUsage
			}

			var one int

			err := operator.Database.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
			if err != nil {
				return err //nolint:wrapcheck // the test reads it as it is.
			}

			_, err = operator.Out.Write([]byte("counted\n"))

			return err //nolint:wrapcheck // the test reads it as it is.
		},
	}}

	out := run(t, cmd, cli.ExitOK, "count")
	if out != "counted\n" {
		t.Errorf("count wrote %q, want %q", out, "counted\n")
	}

	run(t, cmd, cli.ExitUsage, "count", "surplus")
}

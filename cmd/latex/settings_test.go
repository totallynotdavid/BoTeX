package main_test

import (
	"reflect"
	"testing"

	latexcmd "github.com/totallynotdavid/botkit/cmd/latex"
	"github.com/totallynotdavid/botkit/internal/cli"
	"github.com/totallynotdavid/botkit/internal/envfile"
)

// .env.example is written from envfile.Bots, so the binary must read exactly
// the settings, with exactly the defaults, that envfile describes.
func TestBinaryReadsTheSettingsTheExampleDescribes(t *testing.T) {
	t.Parallel()

	for _, bot := range envfile.Bots() {
		if bot.Name != "latex" {
			continue
		}

		if got := cli.Describe(latexcmd.Command()); !reflect.DeepEqual(got, bot.Entries) {
			t.Errorf("the binary reads %+v, envfile describes %+v", got, bot.Entries)
		}

		return
	}

	t.Fatal("envfile does not describe the latex bot")
}

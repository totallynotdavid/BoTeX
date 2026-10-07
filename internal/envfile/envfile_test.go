package envfile_test

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/envfile"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/latex"
)

const examplePath = "../../.env.example"

//nolint:gochecknoglobals // a test flag.
var update = flag.Bool("update", false, "rewrite .env.example")

func example(t *testing.T) string {
	t.Helper()

	text, err := envfile.Example(envfile.Bots())
	if err != nil {
		t.Fatal(err)
	}

	return text
}

func TestExampleIsCurrent(t *testing.T) {
	t.Parallel()

	want := example(t)

	if *update {
		err := os.WriteFile(examplePath, []byte(want), 0o644) //nolint:gosec // the example is meant to be read by everyone.
		if err != nil {
			t.Fatal(err)
		}

		return
	}

	got, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Errorf(".env.example is out of date with the settings the bots read; run `mise run env:example`")
	}
}

// fileEnv reads the settings the example file assigns, the way mise loads it
// into the process.
func fileEnv(t *testing.T) *config.Env {
	t.Helper()

	vars := map[string]string{}

	for line := range strings.SplitSeq(example(t), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			t.Fatalf("line %q is neither a comment nor an assignment", line)
		}

		vars[key] = value
	}

	return config.New(func(key string) (string, bool) {
		value, ok := vars[key]

		return value, ok
	})
}

func emptyEnv() *config.Env {
	return config.New(func(string) (string, bool) { return "", false })
}

// A copy of the example as .env must change nothing: every value in it is
// what the bot would use anyway.
func TestLoadingTheExampleKeepsEveryDefault(t *testing.T) {
	t.Parallel()

	bots := map[string]config.Shared{
		"latex": config.DefaultShared(),
		"flow":  withRateLimit(config.DefaultShared(), flow.DefaultRateLimit()),
	}

	for name, defaults := range bots {
		fromFile, fromNothing := fileEnv(t), emptyEnv()

		if got, want := fromFile.Shared(defaults), fromNothing.Shared(defaults); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: shared settings from the example = %+v, defaults = %+v", name, got, want)
		}
	}

	fromFile := fileEnv(t)

	if got, want := latex.ConfigFromEnv(fromFile), latex.DefaultConfig(); got != want {
		t.Errorf("latex settings from the example = %+v, defaults = %+v", got, want)
	}

	if got, want := flow.ConfigFromEnv(fromFile), flow.ConfigFromEnv(emptyEnv()); got != want {
		t.Errorf("flow settings from the example = %+v, defaults = %+v", got, want)
	}

	err := fromFile.Err()
	if err != nil {
		t.Errorf("the example holds a malformed value: %v", err)
	}
}

func withRateLimit(shared config.Shared, limit config.RateLimit) config.Shared {
	shared.RateLimit = limit

	return shared
}

func TestBotsThatDifferShowTheirDefaultsInANote(t *testing.T) {
	t.Parallel()

	text := example(t)
	shared, own := config.DefaultShared().RateLimit, flow.DefaultRateLimit()

	note := fmt.Sprintf("# Defaults: latex %d, flow %d.\nBOTKIT_RATE_LIMIT_REQUESTS=\n", shared.Requests, own.Requests)
	if !strings.Contains(text, note) {
		t.Errorf("the requests setting is missing %q:\n%s", note, text)
	}

	if !strings.Contains(text, fmt.Sprintf("\nBOTKIT_MAX_IN_FLIGHT=%d\n", config.DefaultShared().MaxInFlight)) {
		t.Errorf("a setting the bots agree on must show its value:\n%s", text)
	}
}

func TestEachBotsSettingsLandInTheRightSection(t *testing.T) {
	t.Parallel()

	sections := map[string]string{}
	heading := ""

	for line := range strings.SplitSeq(example(t), "\n") {
		if strings.HasPrefix(line, "# ") && strings.HasSuffix(line, "---") {
			heading = strings.Fields(line)[1]
		}

		sections[heading] += line + "\n"
	}

	for _, want := range []struct{ section, key, other string }{
		{"latex", latex.KeyTimeout, flow.KeyFile},
		{"flow", flow.KeyFile, latex.KeyTimeout},
		{"Shared", config.KeyStore, latex.KeyTimeout},
	} {
		if !strings.Contains(sections[want.section], "\n"+want.key+"=") {
			t.Errorf("%s is not in the %s section", want.key, want.section)
		}

		if strings.Contains(sections[want.section], "\n"+want.other+"=") {
			t.Errorf("%s is in the %s section", want.other, want.section)
		}
	}
}

func TestLinesFitTheWidth(t *testing.T) {
	t.Parallel()

	for line := range strings.SplitSeq(example(t), "\n") {
		if len(line) > 79 {
			t.Errorf("line of %d columns: %q", len(line), line)
		}
	}
}

func TestHeaderIsAtMostSixLines(t *testing.T) {
	t.Parallel()

	lines := 0

	for line := range strings.SplitSeq(example(t), "\n") {
		if !strings.HasPrefix(line, "#") || strings.HasPrefix(line, "# ---") {
			break
		}

		lines++
	}

	if lines > 6 {
		t.Errorf("header is %d lines; keep it to 6", lines)
	}
}

func TestASettingWithoutAnExplanationIsRefused(t *testing.T) {
	t.Parallel()

	bots := envfile.Bots()
	bots[0].Entries = append(bots[0].Entries, config.Entry{Key: "LATEX_NEW_SETTING", Default: "1"})

	_, err := envfile.Example(bots)
	if !errors.Is(err, envfile.ErrUndocumented) || !strings.Contains(err.Error(), "LATEX_NEW_SETTING") {
		t.Errorf("Example() error = %v, want ErrUndocumented naming LATEX_NEW_SETTING", err)
	}
}

func TestAnExplanationOfASettingNoBotReadsIsRefused(t *testing.T) {
	t.Parallel()

	_, err := envfile.Example(envfile.Bots()[:1])
	if !errors.Is(err, envfile.ErrUnread) || !strings.Contains(err.Error(), flow.KeyFile) {
		t.Errorf("Example() error = %v, want ErrUnread naming %s", err, flow.KeyFile)
	}
}

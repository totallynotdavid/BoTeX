package config_test

import (
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/config"
)

func fromMap(vars map[string]string) *config.Env {
	return config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	})
}

// read is one typed read from an Env, with the value it should produce.
type read struct {
	name string
	get  func(*config.Env) any
	want any
}

func checkReads(t *testing.T, env *config.Env, reads []read) {
	t.Helper()

	for _, r := range reads {
		got := r.get(env)
		if !reflect.DeepEqual(got, r.want) {
			t.Errorf("%s = %#v, want %#v", r.name, got, r.want)
		}
	}

	err := env.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestUnsetAndEmptyUseDefaults(t *testing.T) {
	t.Parallel()

	defaults := []read{
		{"String", func(e *config.Env) any { return e.String("S", "def") }, "def"},
		{"Int", func(e *config.Env) any { return e.Int("I", 7, 1) }, 7},
		{"Duration", func(e *config.Env) any { return e.Duration("D", time.Minute, 0) }, time.Minute},
		{"Bool", func(e *config.Env) any { return e.Bool("B", true) }, true},
		{"Level", func(e *config.Env) any { return e.Level("L", slog.LevelWarn) }, slog.LevelWarn},
		{"List", func(e *config.Env) any { return e.List("LS") }, []string(nil)},
	}

	for name, vars := range map[string]map[string]string{
		"unset": {},
		"empty": {"S": "", "I": "", "D": "", "B": "", "L": "", "LS": ""},
		"blank": {"S": "  ", "I": " ", "D": "\t", "B": " ", "L": " ", "LS": " "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkReads(t, fromMap(vars), defaults)
		})
	}
}

func TestValidValues(t *testing.T) {
	t.Parallel()

	env := fromMap(map[string]string{
		"S":  " /var/lib/botkit/bot.db ",
		"I":  "25",
		"D":  "90s",
		"B":  "TRUE",
		"L":  "debug",
		"LS": " 51900000001@s.whatsapp.net, ,51900000002@s.whatsapp.net,",
	})

	checkReads(t, env, []read{
		{"String", func(e *config.Env) any { return e.String("S", "") }, "/var/lib/botkit/bot.db"},
		{"Int", func(e *config.Env) any { return e.Int("I", 0, 1) }, 25},
		{"Duration", func(e *config.Env) any { return e.Duration("D", 0, time.Second) }, 90 * time.Second},
		{"Bool", func(e *config.Env) any { return e.Bool("B", false) }, true},
		{"Level", func(e *config.Env) any { return e.Level("L", slog.LevelInfo) }, slog.LevelDebug},
		{
			"List", func(e *config.Env) any { return e.List("LS") },
			[]string{"51900000001@s.whatsapp.net", "51900000002@s.whatsapp.net"},
		},
	})
}

func TestMalformedValuesAreAllReported(t *testing.T) {
	t.Parallel()

	env := fromMap(map[string]string{
		"COUNT":   "ten",
		"ZERO":    "0",
		"PERIOD":  "5000", // a bare number has no unit
		"SHORT":   "-1s",
		"ENABLED": "yes",
		"LEVEL":   "verbose",
	})

	// Each read still returns its default so callers can carry on.
	if got := env.Int("COUNT", 5, 1); got != 5 {
		t.Errorf("Int = %d, want the default", got)
	}

	env.Int("ZERO", 5, 1)
	env.Duration("PERIOD", time.Minute, 0)
	env.Duration("SHORT", time.Minute, 0)
	env.Bool("ENABLED", false)
	env.Level("LEVEL", slog.LevelInfo)

	err := env.Err()
	if err == nil {
		t.Fatal("Err() = nil, want every malformed setting")
	}

	for _, want := range []string{
		`COUNT="ten"`, `ZERO="0": below the minimum 1`, `PERIOD="5000"`,
		`SHORT="-1s": below the minimum 0s`, `ENABLED="yes"`, `LEVEL="verbose"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Err() does not mention %s:\n%v", want, err)
		}
	}

	if !errors.Is(err, config.ErrBelowMinimum) {
		t.Error("Err() does not wrap ErrBelowMinimum")
	}
}

func TestEntriesListEachSettingOnceWithItsDefault(t *testing.T) {
	t.Parallel()

	env := fromMap(map[string]string{"I": "99"})

	env.String("S", "def")
	env.Int("I", 7, 1)
	env.Duration("D", 5*time.Minute, 0)
	env.Duration("H", time.Hour, 0)
	env.Duration("MS", 1500*time.Millisecond, 0)
	env.Duration("ZERO", 0, 0)
	env.Bool("B", true)
	env.Level("L", slog.LevelWarn)
	env.List("LS")
	env.Int("I", 8, 1)

	want := []config.Entry{
		{Key: "S", Default: "def"},
		{Key: "I", Default: "7"},
		{Key: "D", Default: "5m"},
		{Key: "H", Default: "1h"},
		{Key: "MS", Default: "1.5s"},
		{Key: "ZERO", Default: "0s"},
		{Key: "B", Default: "true"},
		{Key: "L", Default: "warn"},
		{Key: "LS", Default: ""},
	}

	if got := env.Entries(); !reflect.DeepEqual(got, want) {
		t.Errorf("Entries() = %+v, want %+v", got, want)
	}
}

// An entry's default is one a user can write back: reading it as the value of
// the key gives the default again.
func TestEntryDefaultsRoundTrip(t *testing.T) {
	t.Parallel()

	first := fromMap(nil)
	first.Duration("D", 90*time.Second, 0)
	first.Level("L", slog.LevelError)

	vars := map[string]string{}
	for _, entry := range first.Entries() {
		vars[entry.Key] = entry.Default
	}

	second := fromMap(vars)

	if got := second.Duration("D", 0, 0); got != 90*time.Second {
		t.Errorf("Duration = %v, want 1m30s", got)
	}

	if got := second.Level("L", slog.LevelInfo); got != slog.LevelError {
		t.Errorf("Level = %v, want error", got)
	}

	err := second.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

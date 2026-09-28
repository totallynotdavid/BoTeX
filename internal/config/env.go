// Package config reads settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrBelowMinimum is wrapped by errors for numbers under a setting's minimum.
var ErrBelowMinimum = errors.New("below the minimum")

// Env reads typed settings. A malformed value does not fall back to the
// default: Env records it and keeps going, and Err reports every bad setting
// at once so the process can refuse to start. An empty value counts as unset.
type Env struct {
	lookup func(string) (string, bool)
	errs   []error
}

// FromEnviron reads the process environment.
func FromEnviron() *Env {
	return New(os.LookupEnv)
}

// New reads from lookup, which has the signature of os.LookupEnv.
func New(lookup func(key string) (string, bool)) *Env {
	return &Env{lookup: lookup}
}

// Err joins the errors of every malformed setting read so far.
func (e *Env) Err() error {
	return errors.Join(e.errs...)
}

func (e *Env) String(key, def string) string {
	raw, ok := e.value(key)
	if !ok {
		return def
	}

	return raw
}

// Int reads a base-10 integer no smaller than minimum.
func (e *Env) Int(key string, def, minimum int) int {
	raw, ok := e.value(key)
	if !ok {
		return def
	}

	number, err := strconv.Atoi(raw)
	if err != nil {
		e.fail(key, raw, err)

		return def
	}

	if number < minimum {
		e.fail(key, raw, fmt.Errorf("%w %d", ErrBelowMinimum, minimum))

		return def
	}

	return number
}

// Duration reads a Go duration such as "90s" or "5m", no shorter than minimum.
func (e *Env) Duration(key string, def, minimum time.Duration) time.Duration {
	raw, ok := e.value(key)
	if !ok {
		return def
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		e.fail(key, raw, err)

		return def
	}

	if duration < minimum {
		e.fail(key, raw, fmt.Errorf("%w %s", ErrBelowMinimum, minimum))

		return def
	}

	return duration
}

// Bool reads the forms strconv.ParseBool accepts: 1, t, true, 0, f, false, in
// any case.
func (e *Env) Bool(key string, def bool) bool {
	raw, ok := e.value(key)
	if !ok {
		return def
	}

	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		e.fail(key, raw, err)

		return def
	}

	return enabled
}

// Level reads a slog level name: debug, info, warn or error, in any case.
func (e *Env) Level(key string, def slog.Level) slog.Level {
	raw, ok := e.value(key)
	if !ok {
		return def
	}

	var level slog.Level

	err := level.UnmarshalText([]byte(raw))
	if err != nil {
		e.fail(key, raw, err)

		return def
	}

	return level
}

// List reads comma-separated values, trimmed, with empty items dropped.
func (e *Env) List(key string) []string {
	raw, ok := e.value(key)
	if !ok {
		return nil
	}

	var items []string

	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}

	return items
}

func (e *Env) value(key string) (string, bool) {
	raw, ok := e.lookup(key)
	raw = strings.TrimSpace(raw)

	return raw, ok && raw != ""
}

func (e *Env) fail(key, raw string, err error) {
	e.errs = append(e.errs, fmt.Errorf("%s=%q: %w", key, raw, err))
}

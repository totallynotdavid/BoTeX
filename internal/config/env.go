// Package config reads settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
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
	lookup  func(string) (string, bool)
	errs    []error
	entries []Entry
}

// Entry is a setting the process asked for and the default it used when the
// variable was unset.
type Entry struct {
	Key string
	// Default is the default as a user writes it in the environment: "5m", not
	// "5m0s".
	Default string
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

// Entries lists the settings read so far, once each, in the order of the
// first read. The code that reads a setting is the only place that knows its
// default, so this is how a caller learns the defaults without repeating
// them.
func (e *Env) Entries() []Entry {
	return slices.Clone(e.entries)
}

func (e *Env) String(key, def string) string {
	e.record(key, def)

	raw, ok := e.value(key)
	if !ok {
		return def
	}

	return raw
}

// Int reads a base-10 integer no smaller than minimum.
func (e *Env) Int(key string, def, minimum int) int {
	e.record(key, strconv.Itoa(def))

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
	e.record(key, formatDuration(def))

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
	e.record(key, strconv.FormatBool(def))

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
	e.record(key, strings.ToLower(def.String()))

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

// List reads comma-separated values, trims them, and drops empty items.
func (e *Env) List(key string) []string {
	e.record(key, "")

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

func (e *Env) record(key, def string) {
	if !slices.ContainsFunc(e.entries, func(entry Entry) bool { return entry.Key == key }) {
		e.entries = append(e.entries, Entry{Key: key, Default: def})
	}
}

// formatDuration writes d the short way a person would: "5m" and "1h", not
// "5m0s" and "1h0m0s".
func formatDuration(d time.Duration) string {
	text := d.String()

	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}

	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}

	return text
}

func (e *Env) fail(key, raw string, err error) {
	e.errs = append(e.errs, fmt.Errorf("%s=%q: %w", key, raw, err))
}

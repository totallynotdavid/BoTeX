package typst_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/png" // registers the decoder the tests use to read renders.
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/typst"
)

const (
	dirMode  = 0o700
	fileMode = 0o600

	plainDocument = `#set page(width: auto, height: auto, margin: 8pt)
$ x = (a + b) / c $`

	// countedLoop burns CPU without allocating. A single range(2000000000)
	// would instead abort at once, since typst builds the whole array.
	countedLoop = `#{
  let n = 0
  for i in range(100000) {
    for j in range(100000) { n += 1 }
  }
}`

	stringDoubling = `#{
  let s = "x"
  for i in range(60) { s = s + s }
}`

	widePage = `#set page(width: 5000pt, height: 20pt, margin: 0pt)
wide`
)

// newRunner fails the test when typst or prlimit is missing, so a broken
// toolchain is never mistaken for a pass.
func newRunner(t *testing.T, packagePath string, limits typst.Limits) *typst.Runner {
	t.Helper()

	runner, err := typst.New(packagePath, limits)
	if err != nil {
		t.Fatalf("typst.New: %v", err)
	}

	return runner
}

func withLimits(edit func(*typst.Limits)) typst.Limits {
	limits := typst.DefaultLimits()
	edit(&limits)

	return limits
}

// survivors lists the processes whose command line mentions marker.
func survivors(t *testing.T, marker string) []string {
	t.Helper()

	entries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		t.Fatalf("glob /proc: %v", err)
	}

	var found []string

	for _, entry := range entries {
		cmdline, readErr := os.ReadFile(entry) // #nosec G304 -- a /proc path from Glob.
		if readErr == nil && bytes.Contains(cmdline, []byte(marker)) {
			found = append(found, entry)
		}
	}

	return found
}

type renderCase struct {
	name     string
	document string
	files    map[string]string
	limits   typst.Limits
	// wantErr is the sentinel the error must match. With wantStderr it is
	// instead a render error that quotes wantStderr. With neither, the
	// render must succeed and decode.
	wantErr    error
	wantStderr string
	// within bounds how long the render may take.
	within time.Duration
}

func checkOutcome(t *testing.T, scenario renderCase, png []byte, err error) {
	t.Helper()

	switch {
	case scenario.wantErr != nil:
		if !errors.Is(err, scenario.wantErr) {
			t.Fatalf("err = %v, want %v", err, scenario.wantErr)
		}
	case scenario.wantStderr != "":
		renderErr, ok := errors.AsType[*typst.RenderError](err)
		if !ok {
			t.Fatalf("err = %v, want a *typst.RenderError", err)
		}

		if !strings.Contains(renderErr.Stderr, scenario.wantStderr) {
			t.Fatalf("stderr = %q, want it to contain %q", renderErr.Stderr, scenario.wantStderr)
		}
	default:
		if err != nil {
			t.Fatalf("Render: %v", err)
		}

		_, _, decodeErr := image.Decode(bytes.NewReader(png))
		if decodeErr != nil {
			t.Fatalf("output does not decode as an image: %v", decodeErr)
		}
	}
}

func TestRender(t *testing.T) {
	cases := []renderCase{
		{
			name:     "plain document",
			document: plainDocument,
			limits:   typst.DefaultLimits(),
		},
		{
			name:     "extra file is readable",
			document: `#read("input.tex")`,
			files:    map[string]string{"input.tex": "hello"},
			limits:   typst.DefaultLimits(),
		},
		{
			name:       "file that was not supplied",
			document:   `#read("input.tex")`,
			limits:     typst.DefaultLimits(),
			wantStderr: "file not found",
		},
		{
			name:       "file outside the root",
			document:   `#read("/etc/passwd")`,
			limits:     typst.DefaultLimits(),
			wantStderr: "file not found",
		},
		{
			name:       "typst error quotes stderr",
			document:   `#notafunction()`,
			limits:     typst.DefaultLimits(),
			wantStderr: "unknown variable: notafunction",
		},
		{
			name:       "uncached package is not downloaded",
			document:   `#import "@preview/cetz:0.3.0"`,
			limits:     typst.DefaultLimits(),
			wantStderr: "package",
			within:     time.Second,
		},
		{
			name:     "time limit",
			document: countedLoop,
			limits:   withLimits(func(l *typst.Limits) { l.Timeout = time.Second }),
			wantErr:  typst.ErrLimit,
			within:   time.Second + typst.WaitDelay,
		},
		{
			name:     "memory limit",
			document: stringDoubling,
			limits:   withLimits(func(l *typst.Limits) { l.Data = 256 << 20 }),
			wantErr:  typst.ErrLimit,
		},
		{
			name:     "output file limit",
			document: plainDocument,
			limits:   withLimits(func(l *typst.Limits) { l.FileSize = 1 << 10 }),
			wantErr:  typst.ErrLimit,
		},
		{
			name:     "pixel cap",
			document: widePage,
			limits:   typst.DefaultLimits(),
			wantErr:  typst.ErrTooLarge,
		},
		{
			name:     "byte cap",
			document: plainDocument,
			limits:   withLimits(func(l *typst.Limits) { l.MaxBytes = 1 << 10 }),
			wantErr:  typst.ErrTooLarge,
		},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			// The render directory is created under TMPDIR, so an empty
			// TMPDIR afterwards proves cleanup, and its path finds stragglers.
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)

			runner := newRunner(t, "", scenario.limits)

			start := time.Now()
			png, err := runner.Render(context.Background(), scenario.document, scenario.files)
			elapsed := time.Since(start)

			t.Logf("elapsed %v, err %v", elapsed.Round(time.Millisecond), err)
			checkOutcome(t, scenario, png, err)

			if scenario.within > 0 && elapsed > scenario.within {
				t.Errorf("render took %v, want under %v", elapsed, scenario.within)
			}

			left, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatalf("read TMPDIR: %v", err)
			}

			if len(left) != 0 {
				t.Errorf("render directory not removed: %v", left)
			}

			if found := survivors(t, tmp); len(found) != 0 {
				t.Errorf("processes left behind: %v", found)
			}
		})
	}
}

func TestRenderPackagePath(t *testing.T) {
	t.Parallel()

	packages := t.TempDir()
	pkg := filepath.Join(packages, "preview", "demo", "0.1.0")

	err := os.MkdirAll(pkg, dirMode)
	if err != nil {
		t.Fatal(err)
	}

	manifest := "[package]\nname = \"demo\"\nversion = \"0.1.0\"\nentrypoint = \"lib.typ\"\n"

	for name, content := range map[string]string{
		"typst.toml": manifest,
		"lib.typ":    `#let greet() = [hello from demo]`,
	} {
		err = os.WriteFile(filepath.Join(pkg, name), []byte(content), fileMode)
		if err != nil {
			t.Fatal(err)
		}
	}

	runner := newRunner(t, packages, typst.DefaultLimits())

	_, err = runner.Render(context.Background(), `#import "@preview/demo:0.1.0": greet
#greet()`, nil)
	if err != nil {
		t.Fatalf("Render with a local package: %v", err)
	}
}

func TestRenderCanceled(t *testing.T) {
	t.Parallel()

	runner := newRunner(t, "", typst.DefaultLimits())

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	_, err := runner.Render(ctx, countedLoop, nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, typst.ErrLimit) {
		t.Fatalf("err = %v, want context.Canceled and not ErrLimit", err)
	}
}

func TestRenderRejectsBadFileNames(t *testing.T) {
	t.Parallel()

	runner := newRunner(t, "", typst.DefaultLimits())

	for _, name := range []string{"../escape", "sub/file", "/abs", "main.typ", "out.png", ""} {
		_, err := runner.Render(context.Background(), plainDocument, map[string]string{name: "x"})
		if !errors.Is(err, typst.ErrInvalidFile) {
			t.Errorf("file %q: err = %v, want ErrInvalidFile", name, err)
		}
	}
}

func TestNewRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	fields := map[string]func(*typst.Limits){
		"timeout":    func(l *typst.Limits) { l.Timeout = 0 },
		"data":       func(l *typst.Limits) { l.Data = 0 },
		"file size":  func(l *typst.Limits) { l.FileSize = -1 },
		"max pixels": func(l *typst.Limits) { l.MaxPixels = 0 },
		"max bytes":  func(l *typst.Limits) { l.MaxBytes = 0 },
	}

	for name, edit := range fields {
		_, err := typst.New("", withLimits(edit))
		if !errors.Is(err, typst.ErrInvalidLimits) {
			t.Errorf("%s: err = %v, want ErrInvalidLimits", name, err)
		}
	}
}

func TestNewNamesMissingTools(t *testing.T) {
	typstPath, err := exec.LookPath("typst")
	if err != nil {
		t.Fatalf("typst is not on PATH: %v", err)
	}

	onlyTypst := t.TempDir()

	err = os.Symlink(typstPath, filepath.Join(onlyTypst, "typst"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"no typst", t.TempDir(), "typst"},
		{"no prlimit", onlyTypst, "prlimit"},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("PATH", scenario.path)

			_, err := typst.New("", typst.DefaultLimits())
			if err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("err = %v, want it to name %s", err, scenario.want)
			}
		})
	}
}

// Package typst renders a typst document to a PNG under hard limits on wall
// time, memory and output size.
package typst

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png" // registers the decoder DecodeConfig uses to read the header.
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	mainFile = "main.typ"
	outFile  = "out.png"

	// WaitDelay is how long Render waits after killing a render for its
	// output pipes to close, in case a process escaped the process group.
	WaitDelay = 2 * time.Second

	stderrTailBytes = 4096
	ppi             = "300"
	deadProxy       = "http://127.0.0.1:9"

	mib             = 1 << 20
	defaultPixels   = 4096
	dirMode         = 0o700
	fileMode        = 0o600
	defaultTimeout  = 10 * time.Second
	defaultData     = 256 * mib
	defaultFileSize = 16 * mib
	defaultBytes    = 5 * mib
)

var (
	// ErrLimit means a resource limit stopped the render: the deadline, a
	// fatal signal from the limits, or an allocation failure.
	ErrLimit = errors.New("render exceeded a resource limit")

	// ErrTooLarge means the render finished but its image is over the pixel
	// or byte cap.
	ErrTooLarge = errors.New("rendered image is too large")

	// ErrInvalidLimits is returned by New for a limit that is not positive.
	ErrInvalidLimits = errors.New("invalid limits")

	// ErrInvalidFile is returned by Render for an extra file name that is not
	// a plain file name in the render directory.
	ErrInvalidFile = errors.New("invalid file name")
)

// RenderError is a render that failed for a reason other than a limit, such as
// a typst syntax error.
type RenderError struct {
	// Stderr is the tail of typst's standard error.
	Stderr string
	Err    error
}

func (e *RenderError) Error() string {
	return fmt.Sprintf("typst compile: %v: %s", e.Err, e.Stderr)
}

func (e *RenderError) Unwrap() error {
	return e.Err
}

// Limits bound one render.
type Limits struct {
	// Timeout is the wall time. Typst runs with one job, so it is also the
	// CPU bound.
	Timeout time.Duration
	// Data is RLIMIT_DATA in bytes. RLIMIT_AS is not used: typst reserves
	// virtual memory per thread, so it fails good renders.
	Data int64
	// FileSize is RLIMIT_FSIZE in bytes, the most typst may write to a file.
	FileSize int64
	// MaxPixels is the most pixels either side of the image may have.
	MaxPixels int
	// MaxBytes is the most bytes the PNG may have.
	MaxBytes int64
}

// DefaultLimits returns the limits a chat command renders under.
func DefaultLimits() Limits {
	return Limits{
		Timeout:   defaultTimeout,
		Data:      defaultData,
		FileSize:  defaultFileSize,
		MaxPixels: defaultPixels,
		MaxBytes:  defaultBytes,
	}
}

func (l Limits) validate() error {
	switch {
	case l.Timeout <= 0:
		return fmt.Errorf("%w: timeout %v", ErrInvalidLimits, l.Timeout)
	case l.Data <= 0:
		return fmt.Errorf("%w: data %d", ErrInvalidLimits, l.Data)
	case l.FileSize <= 0:
		return fmt.Errorf("%w: file size %d", ErrInvalidLimits, l.FileSize)
	case l.MaxPixels <= 0:
		return fmt.Errorf("%w: max pixels %d", ErrInvalidLimits, l.MaxPixels)
	case l.MaxBytes <= 0:
		return fmt.Errorf("%w: max bytes %d", ErrInvalidLimits, l.MaxBytes)
	}

	return nil
}

// Runner renders typst documents. It is safe for concurrent use.
type Runner struct {
	limits      Limits
	packagePath string
	typst       string
	prlimit     string
}

// New finds typst and prlimit (util-linux) on PATH. packagePath is a
// directory of local typst packages, or empty for none. Packages are never
// downloaded.
func New(packagePath string, limits Limits) (*Runner, error) {
	err := limits.validate()
	if err != nil {
		return nil, err
	}

	typst, err := exec.LookPath("typst")
	if err != nil {
		return nil, fmt.Errorf("typst is required: %w", err)
	}

	prlimit, err := exec.LookPath("prlimit")
	if err != nil {
		return nil, fmt.Errorf("prlimit (util-linux) is required: %w", err)
	}

	if packagePath != "" {
		packagePath, err = filepath.Abs(packagePath)
		if err != nil {
			return nil, fmt.Errorf("resolve package path: %w", err)
		}
	}

	return &Runner{limits: limits, packagePath: packagePath, typst: typst, prlimit: prlimit}, nil
}

// Render compiles document to a PNG. files are extra files the document can
// read, by plain file name. Typst can read nothing outside them.
//
// A render stopped by a limit returns ErrLimit, one over the size caps
// returns ErrTooLarge, and any other failure returns a *RenderError with
// typst's message. No process outlives the call.
func (r *Runner) Render(ctx context.Context, document string, files map[string]string) (png []byte, err error) {
	for name := range files {
		if !filepath.IsLocal(name) || filepath.Base(name) != name || name == mainFile || name == outFile {
			return nil, fmt.Errorf("%w: %q", ErrInvalidFile, name)
		}
	}

	dir, err := os.MkdirTemp("", "botkit-typst-")
	if err != nil {
		return nil, fmt.Errorf("create render directory: %w", err)
	}

	defer func() {
		err = errors.Join(err, os.RemoveAll(dir))
	}()

	cacheDir := filepath.Join(dir, "empty")

	err = os.Mkdir(cacheDir, dirMode)
	if err != nil {
		return nil, fmt.Errorf("create package cache directory: %w", err)
	}

	err = writeFiles(dir, document, files)
	if err != nil {
		return nil, err
	}

	err = r.compile(ctx, dir, cacheDir)
	if err != nil {
		return nil, err
	}

	return r.readImage(filepath.Join(dir, outFile))
}

func writeFiles(dir, document string, files map[string]string) error {
	all := map[string]string{mainFile: document}
	maps.Copy(all, files)

	for name, content := range all {
		err := os.WriteFile(filepath.Join(dir, name), []byte(content), fileMode)
		if err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	return nil
}

func (r *Runner) compile(ctx context.Context, dir, cacheDir string) error {
	ctx, cancel := context.WithTimeout(ctx, r.limits.Timeout)
	defer cancel()

	args := []string{
		"--data=" + strconv.FormatInt(r.limits.Data, 10),
		"--fsize=" + strconv.FormatInt(r.limits.FileSize, 10),
		"--", r.typst, "compile",
		"--root", dir,
		"--package-cache-path", cacheDir,
		"--ignore-system-fonts",
		"--jobs", "1",
		"--format", "png",
		"--ppi", ppi,
	}
	if r.packagePath != "" {
		args = append(args, "--package-path", r.packagePath)
	}

	args = append(args, mainFile, outFile)

	cmd := exec.CommandContext(ctx, r.prlimit, args...) // #nosec G204 -- both binaries come from PATH lookups and args are fixed.
	cmd.Dir = dir
	// Typst has no offline flag but honours the proxy variables, so a dead
	// proxy makes any package download fail at once.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + dir,
		"HTTPS_PROXY=" + deadProxy, "HTTP_PROXY=" + deadProxy,
		"https_proxy=" + deadProxy, "http_proxy=" + deadProxy,
	}

	// Killing only typst would leave anything it spawned holding the pipes;
	// the group dies together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}

		if err != nil {
			return fmt.Errorf("kill process group: %w", err)
		}

		return nil
	}
	cmd.WaitDelay = WaitDelay

	tail := &tailWriter{max: stderrTailBytes}
	cmd.Stderr = tail

	runErr := cmd.Run()
	if runErr == nil {
		return nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: no result within %v", ErrLimit, r.limits.Timeout)
	}

	if ctx.Err() != nil {
		return fmt.Errorf("render canceled: %w", ctx.Err())
	}

	stderr := strings.TrimSpace(tail.String())

	if reason := limitReason(runErr, stderr); reason != "" {
		return fmt.Errorf("%w: %s", ErrLimit, reason)
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		return &RenderError{Stderr: stderr, Err: exitErr}
	}

	return fmt.Errorf("run typst: %w", runErr)
}

// limitReason names the limit that stopped a process, judged from how it
// ended, or returns "" when it ended on its own.
func limitReason(runErr error, stderr string) string {
	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		status, isWait := exitErr.Sys().(syscall.WaitStatus)
		if isWait && status.Signaled() {
			sig := status.Signal()
			if sig == syscall.SIGKILL || sig == syscall.SIGXFSZ || sig == syscall.SIGABRT {
				return "typst stopped by " + sig.String()
			}
		}
	}

	if strings.Contains(stderr, "memory allocation of") {
		return "typst ran out of memory"
	}

	return ""
}

// readImage returns the PNG at path if it is within the pixel and byte caps.
func (r *Runner) readImage(path string) ([]byte, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is inside the private render directory.
	if err != nil {
		return nil, fmt.Errorf("read rendered image: %w", err)
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode rendered image: %w", err)
	}

	if config.Width > r.limits.MaxPixels || config.Height > r.limits.MaxPixels {
		return nil, fmt.Errorf("%w: %dx%d px, at most %d per side", ErrTooLarge, config.Width, config.Height, r.limits.MaxPixels)
	}

	if int64(len(data)) > r.limits.MaxBytes {
		return nil, fmt.Errorf("%w: %d bytes, at most %d", ErrTooLarge, len(data), r.limits.MaxBytes)
	}

	return data, nil
}

// tailWriter keeps the last max bytes written to it. Writes never fail, so a
// noisy process cannot stall or grow the parent.
type tailWriter struct {
	max int
	buf []byte
}

func (t *tailWriter) Write(chunk []byte) (int, error) {
	t.buf = append(t.buf, chunk...)
	if extra := len(t.buf) - t.max; extra > 0 {
		t.buf = bytes.Clone(t.buf[extra:])
	}

	return len(chunk), nil
}

func (t *tailWriter) String() string {
	return string(t.buf)
}

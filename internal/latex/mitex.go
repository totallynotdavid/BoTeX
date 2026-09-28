package latex

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// mitexPackages is the typst package tree with mitex 0.2.7 vendored from
// https://packages.typst.org/preview/mitex-0.2.7.tar.gz, without its README
// and CHANGELOG. Its layout is what typst expects under --package-path.
//
//go:embed mitex
var mitexPackages embed.FS

const (
	mitexRoot = "mitex"
	dirMode   = 0o700
	fileMode  = 0o600
)

// extractMitex writes the vendored packages to a private temporary directory
// and returns it, to be passed to typst as its package path. cleanup removes
// the directory.
func extractMitex() (dir string, cleanup func() error, err error) {
	dir, err = os.MkdirTemp("", "botkit-mitex-")
	if err != nil {
		return "", nil, fmt.Errorf("create package directory: %w", err)
	}

	cleanup = func() error { return os.RemoveAll(dir) }

	err = fs.WalkDir(mitexPackages, mitexRoot, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		target := filepath.Join(dir, filepath.FromSlash(name[len(mitexRoot):]))
		if entry.IsDir() {
			return os.MkdirAll(target, dirMode)
		}

		content, readErr := mitexPackages.ReadFile(name)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", name, readErr)
		}

		return os.WriteFile(target, content, fileMode)
	})
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("extract mitex: %w", err), cleanup())
	}

	return dir, cleanup, nil
}

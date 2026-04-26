package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
)

// resolveHost finds the host repo for the current working directory.
func resolveHost() (host.Repo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return host.Repo{}, fmt.Errorf("getwd: %w", err)
	}
	return host.Detect(cwd)
}

// openOverlay finds the host repo and resolves the bare-repo path. Returns an error if no overlay has been initialised.
func openOverlay() (host.Repo, gitwrap.Repo, error) {
	hr, err := resolveHost()
	if err != nil {
		return hr, gitwrap.Repo{}, err
	}
	bare, err := store.BareDir(hr.Fingerprint())
	if err != nil {
		return hr, gitwrap.Repo{}, err
	}
	if _, err := os.Stat(bare); err != nil {
		if os.IsNotExist(err) {
			return hr, gitwrap.Repo{}, fmt.Errorf("no overlay for %s — run `attic init` or `attic clone <remote>`", hr.Root)
		}
		return hr, gitwrap.Repo{}, fmt.Errorf("stat overlay %s: %w", bare, err)
	}
	return hr, gitwrap.Repo{GitDir: bare, WorkTree: hr.Root}, nil
}

// gitignorePath returns the absolute path to the host repo's .gitignore.
func gitignorePath(hr host.Repo) string {
	return filepath.Join(hr.Root, ".gitignore")
}

// relativiseToHost converts a list of user-supplied paths into clean, slash-separated
// paths relative to the host repo root. It refuses paths outside the host root.
func relativiseToHost(hostRoot string, args []string) ([]string, error) {
	rels := make([]string, 0, len(args))
	for _, a := range args {
		abs, err := filepath.Abs(a)
		if err != nil {
			return nil, fmt.Errorf("path %s: %w", a, err)
		}
		// Resolve symlinks when the path exists; otherwise fall back to the cleaned absolute path.
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		rel, err := filepath.Rel(hostRoot, abs)
		if err != nil {
			return nil, fmt.Errorf("path %s: %w", a, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("path %s is outside host repo %s", a, hostRoot)
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	return rels, nil
}

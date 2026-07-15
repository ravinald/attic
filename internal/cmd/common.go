package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
)

// overlayBranchPrefix names an overlay's branch on the mono remote. The fingerprint identifies the
// git repo the overlay backs, so "repo/" reads truer than the original "host/".
const overlayBranchPrefix = "repo/"

// overlayBranch is the mono-remote branch name for an overlay fingerprint.
func overlayBranch(fp string) string { return overlayBranchPrefix + fp }

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

// hostGit runs a git subcommand against the host repo itself, not the overlay. It uses
// -C rather than a hardcoded --git-dir so git's own discovery stays correct when .git is
// a file (linked worktrees) or core.hooksPath/GIT_DIR is customised. Stderr passes
// through so git's diagnostics reach the user, matching gitwrap.
func hostGit(hostRoot string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", hostRoot}, args...)...)
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return string(out), fmt.Errorf("host git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// ejectFromHost removes rels from the HOST repo's index so an overlay-managed path stops
// being tracked or staged upstream. A .gitignore rule cannot untrack a path already in
// the index nor stop a `git add -f`, so eviction has to be explicit. --cached leaves the
// working-tree files in place (the overlay still owns them); --ignore-unmatch makes the
// call a no-op when the path was never tracked.
func ejectFromHost(hr host.Repo, rels []string) error {
	if len(rels) == 0 {
		return nil
	}
	args := append([]string{"rm", "-r", "--cached", "--ignore-unmatch", "--quiet", "--"}, rels...)
	if _, err := hostGit(hr.Root, args...); err != nil {
		return fmt.Errorf("eject from host index: %w", err)
	}
	return nil
}

// topLevels reduces repo-relative paths to their unique first path segments — the
// granularity attic records in the .gitignore block and evicts from the host index.
func topLevels(paths []string) []string {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if i := strings.IndexByte(p, '/'); i >= 0 {
			p = p[:i]
		}
		set[p] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
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

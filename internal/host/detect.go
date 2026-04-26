// Package host discovers the git repository associated with a working directory and computes a stable identity for it.
package host

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Repo describes a host git repository discovered relative to a working directory.
type Repo struct {
	Root      string // absolute path to the work tree root
	RootSHA   string // smallest root commit SHA (full)
	OriginURL string // origin remote URL, "" if absent
}

// Detect discovers the host repo containing dir. It requires the repo to have at least one commit.
func Detect(dir string) (Repo, error) {
	var r Repo
	root, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return r, fmt.Errorf("host: not inside a git repository (cwd=%s): %w", dir, err)
	}
	// Canonicalise so path comparisons in `add`/`rm` survive symlinks (e.g. macOS /var → /private/var).
	canonical, err := filepath.EvalSymlinks(strings.TrimSpace(root))
	if err != nil {
		return r, fmt.Errorf("host: resolve symlinks for %s: %w", root, err)
	}
	r.Root = canonical

	out, err := runGit(r.Root, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return r, fmt.Errorf("host: cannot read root commit in %s — repo has no commits?: %w", r.Root, err)
	}
	shas := strings.Fields(strings.TrimSpace(out))
	if len(shas) == 0 {
		return r, fmt.Errorf("host: no root commits in %s", r.Root)
	}
	sort.Strings(shas)
	r.RootSHA = shas[0]

	if out, err := runGit(r.Root, "remote", "get-url", "origin"); err == nil {
		r.OriginURL = strings.TrimSpace(out)
	}
	return r, nil
}

// Fingerprint returns the 12-char identifier used to key this repo's overlay storage.
func (r Repo) Fingerprint() string {
	if len(r.RootSHA) < 12 {
		return r.RootSHA
	}
	return r.RootSHA[:12]
}

// Name returns the host repo's basename, suitable for naming a remote.
func (r Repo) Name() string {
	if r.Root == "" {
		return ""
	}
	parts := strings.Split(strings.TrimRight(r.Root, "/"), "/")
	return parts[len(parts)-1]
}

func runGit(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

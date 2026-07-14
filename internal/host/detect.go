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

// OwnerRepo parses this repo's origin URL into an "owner/repo" slug. ok is false when there is no
// origin or the URL can't be parsed into a slug.
func (r Repo) OwnerRepo() (string, bool) {
	return ParseOwnerRepo(r.OriginURL)
}

// ParseOwnerRepo reduces a git remote URL to its "owner/repo" path, stripping scheme, host, and a
// trailing ".git". It returns ok=false for empty or unparseable URLs. GitLab subgroups yield a
// multi-segment slug (group/sub/repo) — kept as-is, since it's still the unambiguous project path.
func ParseOwnerRepo(url string) (string, bool) {
	_, path, ok := splitRemote(url)
	return path, ok
}

// WebBase converts a git remote URL to its https browse root ("https://host/owner/repo"). It returns
// ok=false when the URL can't be parsed. Used to build clickable links for the mono-remote map.
func WebBase(url string) (string, bool) {
	host, path, ok := splitRemote(url)
	if !ok {
		return "", false
	}
	return "https://" + host + "/" + path, true
}

// splitRemote breaks a git remote URL into its host and "owner/repo" path, tolerating both scp-like
// (git@host:owner/repo.git) and URL (https://host/owner/repo.git, ssh://git@host/owner/repo) shapes.
func splitRemote(raw string) (host, path string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	raw = strings.TrimSuffix(strings.TrimRight(raw, "/"), ".git")

	if _, rest, found := strings.Cut(raw, "://"); found {
		hostPart, p, ok := strings.Cut(rest, "/")
		if !ok {
			return "", "", false
		}
		if _, h, ok := strings.Cut(hostPart, "@"); ok {
			hostPart = h // drop userinfo (ssh://git@host/...)
		}
		host, path = hostPart, strings.TrimLeft(p, "/")
	} else if _, rest, ok := strings.Cut(raw, "@"); ok {
		host, path, ok = strings.Cut(rest, ":") // host:owner/repo
		if !ok {
			return "", "", false
		}
		path = strings.TrimLeft(path, "/")
	} else {
		return "", "", false
	}

	if host == "" || path == "" || strings.IndexByte(path, '/') < 0 {
		return "", "", false
	}
	for _, r := range path {
		if r == ' ' || r == '\t' {
			return "", "", false
		}
	}
	return host, path, true
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

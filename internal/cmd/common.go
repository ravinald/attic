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
	"github.com/ravinald/attic/internal/ignore"
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
	// Overlays created before the exclude existed would otherwise stay noisy forever, so heal here
	// rather than only at init — this is the one path every overlay command goes through.
	if err := ensureOverlayExclude(bare); err != nil {
		return hr, gitwrap.Repo{}, err
	}
	return hr, gitwrap.Repo{GitDir: bare, WorkTree: hr.Root}, nil
}

// excludeAll suppresses every path the overlay has not explicitly added.
const excludeAll = "/*"

// ensureOverlayExclude writes attic's block into the overlay's info/exclude. An overlay's work tree
// is the *entire* host repo, so with no exclude every host file reads as untracked: `attic status`
// buries the one real change under the host's whole tree, `attic commit` dies with "nothing added to
// commit but untracked files present", and `attic exec -- add -A` would swallow the host repo.
// Overlay paths reach the index through the `git add --force` in `attic add`, which outranks this.
func ensureOverlayExclude(bare string) error {
	path := filepath.Join(bare, "info", "exclude")
	blk, err := ignore.Load(path)
	if err != nil {
		return err
	}
	if blk.Has(excludeAll) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("overlay exclude: mkdir %s: %w", filepath.Dir(path), err)
	}
	blk.Add(excludeAll)
	return blk.Save()
}

// overlayScope returns the host-relative top-level paths the overlay owns. Either source alone
// lies: the .gitignore block can name a path with nothing committed under it yet, and a clone
// restores tracked files before it rewrites the block — so take the union.
func overlayScope(hr host.Repo, repo gitwrap.Repo) ([]string, error) {
	blk, err := ignore.Load(gitignorePath(hr))
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, l := range blk.Lines {
		// A negation re-includes a path the block already claims; as a pathspec it would mean a
		// literal file named "!foo".
		if p := strings.Trim(l, "/"); p != "" && !strings.HasPrefix(p, "!") {
			paths = append(paths, p)
		}
	}
	out, err := repo.Run("ls-files")
	if err != nil {
		return nil, err
	}
	paths = append(paths, splitLines(out)...)
	return topLevels(paths), nil
}

// untrackedOverlayFiles lists files under the overlay's scope that git sees as untracked. They are
// ignored files by construction — the host .gitignore hides exactly the paths the overlay owns, and
// it outranks the overlay's own exclude — so a plain `git status` will never volunteer them. attic
// has to ask by name or a new changelog stays invisible until someone notices it never got pushed.
func untrackedOverlayFiles(repo gitwrap.Repo, scope []string) ([]string, error) {
	if len(scope) == 0 {
		return nil, nil
	}
	args := append([]string{"ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--"}, scope...)
	out, err := repo.Run(args...)
	if err != nil {
		return nil, err
	}
	var files []string
	for f := range strings.SplitSeq(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files, nil
}

// splitLines splits git's newline-delimited output, dropping the trailing empty element.
func splitLines(out string) []string {
	var lines []string
	for l := range strings.SplitSeq(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
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

// onDuplicateEnv overrides the on_duplicate policy for a single invocation, below a command flag but
// above any persisted config.
const onDuplicateEnv = "ATTIC_GITIGNORE_ON_DUPLICATE"

// onDuplicatePerRepo returns the current host repo's per-repo on_duplicate override, or "" when there
// is no overlay or no override. Resolution failures collapse to "" — a missing overlay must not block
// reading the policy, and the higher layers still apply.
func onDuplicatePerRepo() string {
	hr, err := resolveHost()
	if err != nil {
		return ""
	}
	m, err := store.LoadMeta(hr.Fingerprint())
	if err != nil {
		return ""
	}
	return m.GitignoreOnDuplicate
}

// envOnDuplicate returns the on_duplicate override from the environment, or "" when unset.
func envOnDuplicate() string { return os.Getenv(onDuplicateEnv) }

// resolveOnDuplicate resolves the effective on_duplicate policy: flag > env > per-repo > global > default.
func resolveOnDuplicate(flag string) (string, error) {
	global, err := store.LoadConfig()
	if err != nil {
		return "", err
	}
	return store.ResolveOnDuplicate(flag, envOnDuplicate(), onDuplicatePerRepo(), global)
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

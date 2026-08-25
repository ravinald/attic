package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
)

// TestCloneRefusesMonoRemoteWithoutFlag is the regression for the wedge that motivated these guards.
// `attic clone <mono-url>` with no --mono took git's whole-bare path: it cloned every project's
// history, landed on the remote's default branch (the label map, not this repo's overlay), and checked
// that branch's README.md out over the host repo's own. The clone must refuse and name the flag.
func TestCloneRefusesMonoRemoteWithoutFlag(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{"docs-internal/a.md": "doc"})
	setCloneFlags(t, false, false)

	err := cloneCmd.RunE(cloneCmd, []string{fx.remote})
	if err == nil {
		t.Fatal("clone accepted a mono remote without --mono")
	}
	if !strings.Contains(err.Error(), "--mono") {
		t.Errorf("error does not name the flag: %v", err)
	}
	if _, statErr := os.Stat(fx.bare); !os.IsNotExist(statErr) {
		t.Errorf("refused clone left a bare behind at %s", fx.bare)
	}
}

// A per-host-repo remote carries no repo/<fp> or _attic/labels refs, so the guard must let it through.
// Without this the fix would break the default mode it was never meant to touch.
func TestCloneAcceptsPlainRemoteWithoutFlag(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{"docs-internal/a.md": "doc"})
	solo := newSoloRemote(t, map[string]string{"docs-internal/a.md": "doc"})
	setCloneFlags(t, false, false)

	if err := cloneCmd.RunE(cloneCmd, []string{solo}); err != nil {
		t.Fatalf("clone refused a plain remote: %v", err)
	}
	if _, err := os.Stat(fx.bare); err != nil {
		t.Errorf("clone of a plain remote provisioned no bare: %v", err)
	}
}

// TestCloneForceRefusesHostTrackedCollision covers the damage the mono mix-up actually did: a
// committed README.md overwritten by one from the overlay remote. --force is for reclaiming stray
// untracked copies of overlay files; a path the host repo tracks has an owner and stays untouched.
func TestCloneForceRefusesHostTrackedCollision(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{
		"README.md":          "# from the overlay remote",
		"docs-internal/a.md": "doc",
	})
	setCloneFlags(t, true, true)

	err := cloneCmd.RunE(cloneCmd, []string{fx.remote})
	if err == nil {
		t.Fatal("clone --force overwrote a host-tracked path")
	}
	if !strings.Contains(err.Error(), "tracked by the host repo") {
		t.Errorf("error does not explain the refusal: %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(fx.hostRoot, "README.md"))
	if readErr != nil {
		t.Fatalf("read host README: %v", readErr)
	}
	if got := string(body); got != hostReadme {
		t.Errorf("host README.md = %q, want %q", got, hostReadme)
	}
	if _, statErr := os.Stat(fx.bare); !os.IsNotExist(statErr) {
		t.Errorf("refused clone left a bare behind at %s", fx.bare)
	}
}

// The narrower --force still has to work: an untracked stray copy of an overlay file is exactly what
// the flag exists to overwrite, and the host-tracked guard must not swallow that case too.
func TestCloneForceOverwritesUntrackedCollision(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{"docs-internal/a.md": "from the overlay"})
	writeFile(t, fx.hostRoot, "docs-internal/a.md", "stray local copy")
	setCloneFlags(t, true, true)

	if err := cloneCmd.RunE(cloneCmd, []string{fx.remote}); err != nil {
		t.Fatalf("clone --mono --force: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(fx.hostRoot, "docs-internal", "a.md"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if got := string(body); got != "from the overlay" {
		t.Errorf("restored file = %q, want the overlay's content", got)
	}
}

// A mono clone must scope its refspec to its own branch, or every later fetch drags the whole store in.
func TestCloneMonoScopesFetchRefspec(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{"docs-internal/a.md": "doc"})
	setCloneFlags(t, true, false)

	if err := cloneCmd.RunE(cloneCmd, []string{fx.remote}); err != nil {
		t.Fatalf("clone --mono: %v", err)
	}
	repo := gitwrap.Repo{GitDir: fx.bare, WorkTree: fx.hostRoot}
	got, err := repo.Run("config", "--get", "remote.origin.fetch")
	if err != nil {
		t.Fatalf("config fetch: %v", err)
	}
	want := monoFetchRefspec(overlayBranch(fx.fp))
	if strings.TrimSpace(got) != want {
		t.Errorf("fetch refspec = %q, want %q", strings.TrimSpace(got), want)
	}
	// The unrelated project's branch must not have come along for the ride.
	if refs, err := repo.Run("for-each-ref", "--format=%(refname)", "refs/remotes/"); err != nil {
		t.Fatalf("for-each-ref: %v", err)
	} else if strings.Contains(refs, otherFP) {
		t.Errorf("mono clone pulled another project's branch:\n%s", refs)
	}
}

// narrowMonoFetch is what heals an overlay wired before the refspec was scoped. fetch and pull call it;
// this pins that a wildcard gets narrowed and that a non-mono overlay's wildcard is left alone.
func TestNarrowMonoFetch(t *testing.T) {
	cases := []struct {
		name   string
		branch string
		want   string
	}{
		{"mono overlay is narrowed", "repo/deadbeefcafe", "+refs/heads/repo/deadbeefcafe:refs/remotes/origin/repo/deadbeefcafe"},
		{"solo overlay keeps the wildcard", "main", wildcardRefspec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bare := filepath.Join(t.TempDir(), "attic.git")
			if err := (gitwrap.Repo{}).Stream("init", "--bare", "-q", "-b", tc.branch, bare); err != nil {
				t.Fatalf("init bare: %v", err)
			}
			repo := gitwrap.Repo{GitDir: bare}
			if err := repo.Stream("remote", "add", "origin", "https://example.invalid/mono"); err != nil {
				t.Fatalf("remote add: %v", err)
			}
			if err := narrowMonoFetch(repo); err != nil {
				t.Fatalf("narrowMonoFetch: %v", err)
			}
			got, err := repo.Run("config", "--get", "remote.origin.fetch")
			if err != nil {
				t.Fatalf("config fetch: %v", err)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Errorf("fetch refspec = %q, want %q", strings.TrimSpace(got), tc.want)
			}
		})
	}
}

const (
	hostReadme = "# the host repo's own readme"
	// otherFP stands in for another project's overlay on the same mono remote.
	otherFP = "0123456789ab"
	// wildcardRefspec is what `git remote add` writes, and what a mono overlay must not keep.
	wildcardRefspec = "+refs/heads/*:refs/remotes/origin/*"
)

type cloneFixture struct {
	hostRoot string
	fp       string
	bare     string
	remote   string
}

// newCloneFixture builds a host repo with a commit and a local repo shaped like a shared mono remote.
// setCloneFlags plus t.Chdir make cloneCmd.RunE runnable directly, as the rekey tests do.
func newCloneFixture(t *testing.T, overlayFiles map[string]string) cloneFixture {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	hostDir := t.TempDir()
	writeFile(t, hostDir, "README.md", hostReadme)
	git(t, hostDir, "init", "-q", ".")
	git(t, hostDir, "add", "-A")
	git(t, hostDir, "commit", "-qm", "init")

	hr, err := host.Detect(hostDir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	fp := hr.Fingerprint()
	bare, err := store.BareDir(fp)
	if err != nil {
		t.Fatal(err)
	}
	// The label branch carries a README.md of its own: that file landing on the host repo's is the
	// damage a whole-bare clone of a mono remote does.
	labelFiles := map[string]string{"README.md": "# attic overlays", "labels.toml": "[hosts]\n"}
	t.Chdir(hr.Root)
	return cloneFixture{
		hostRoot: hr.Root,
		fp:       fp,
		bare:     bare,
		remote:   newMonoRemote(t, fp, overlayFiles, labelFiles),
	}
}

// newMonoRemote builds a repo with the ref layout of a shared mono remote: one orphan branch per
// overlay fingerprint plus the label branch, with HEAD left on the label branch — which is what makes
// a whole-bare clone of one check the wrong files out over the host repo.
func newMonoRemote(t *testing.T, fp string, overlayFiles, labelFiles map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", labelsBranch, ".")
	commitTree(t, dir, labelFiles, "labels")

	for _, b := range []string{overlayBranch(fp), overlayBranch(otherFP)} {
		git(t, dir, "checkout", "-q", "--orphan", b)
		git(t, dir, "rm", "-rq", "--cached", "--ignore-unmatch", ".")
		removeWorkTree(t, dir)
		commitTree(t, dir, overlayFiles, "overlay")
	}
	git(t, dir, "symbolic-ref", "HEAD", "refs/heads/"+labelsBranch)
	git(t, dir, "checkout", "-qf", labelsBranch)
	return dir
}

// newSoloRemote builds a per-host-repo remote: a single branch, no repo/<fp> or label refs.
func newSoloRemote(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main", ".")
	commitTree(t, dir, files, "overlay")
	return dir
}

func commitTree(t *testing.T, dir string, files map[string]string, msg string) {
	t.Helper()
	for rel, body := range files {
		writeFile(t, dir, rel, body)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", msg)
}

// removeWorkTree clears everything but .git so an orphan branch starts from an empty tree.
func removeWorkTree(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

// setCloneFlags sets the command's package-level flags and restores them, so ordering between tests
// cannot leak a --force into one that asserts a refusal.
func setCloneFlags(t *testing.T, mono, force bool) {
	t.Helper()
	prev := cloneFlags
	t.Cleanup(func() { cloneFlags = prev })
	cloneFlags.mono = mono
	cloneFlags.force = force
}

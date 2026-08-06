package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
)

// TestRekeyMovesStorageAndRenamesMonoBranch is the regression for the failure mode rekey exists to
// undo: a host history rewrite changes the root commit, attic's key moves with it, and the overlay
// becomes unreachable while its history sits intact on disk.
func TestRekeyMovesStorageAndRenamesMonoBranch(t *testing.T) {
	fx := newRekeyFixture(t)

	if fx.oldFP == fx.newFP {
		t.Fatal("fixture did not change the root commit, so there is nothing to re-key")
	}

	var out bytes.Buffer
	rekeyCmd.SetOut(&out)
	rekeyFlags.dryRun = false
	if err := rekeyCmd.RunE(rekeyCmd, nil); err != nil {
		t.Fatalf("rekey: %v", err)
	}

	oldDir, _ := store.RepoDir(fx.oldFP)
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old storage %s still present", oldDir)
	}
	newBare, _ := store.BareDir(fx.newFP)
	if _, err := os.Stat(newBare); err != nil {
		t.Fatalf("new storage missing at %s: %v", newBare, err)
	}

	m, err := store.LoadMeta(fx.newFP)
	if err != nil {
		t.Fatalf("load meta: %v", err)
	}
	if m.Fingerprint != fx.newFP {
		t.Errorf("meta fingerprint = %q, want %q", m.Fingerprint, fx.newFP)
	}
	if want := "repo/" + fx.newFP; m.Branch != want {
		t.Errorf("meta branch = %q, want %q", m.Branch, want)
	}
	if m.HostRoot != fx.hostRoot {
		t.Errorf("meta host_root = %q, want %q", m.HostRoot, fx.hostRoot)
	}

	repo := gitwrap.Repo{GitDir: newBare, WorkTree: fx.hostRoot}
	refs, err := repo.Run("show-ref")
	if err != nil {
		t.Fatalf("show-ref: %v", err)
	}
	if !strings.Contains(refs, "refs/heads/repo/"+fx.newFP) {
		t.Errorf("new branch missing:\n%s", refs)
	}
	if strings.Contains(refs, "refs/heads/repo/"+fx.oldFP) {
		t.Errorf("old branch survived the rename:\n%s", refs)
	}

	// The overlay's own commit must be untouched: rekey re-labels, it never rewrites history.
	if body, err := repo.Run("show", "-s", "--format=%s", "refs/heads/repo/"+fx.newFP); err != nil {
		t.Fatalf("show: %v", err)
	} else if got := strings.TrimSpace(body); got != "overlay" {
		t.Errorf("overlay tip subject = %q, want %q", got, "overlay")
	}

	fetch, err := repo.Run("config", "--get", "remote.origin.fetch")
	if err != nil {
		t.Fatalf("config fetch: %v", err)
	}
	wantFetch := "+refs/heads/repo/" + fx.newFP + ":refs/remotes/origin/repo/" + fx.newFP
	if strings.TrimSpace(fetch) != wantFetch {
		t.Errorf("fetch refspec = %q, want %q", strings.TrimSpace(fetch), wantFetch)
	}
}

// TestRekeyDryRunChangesNothing guards the flag people will reach for first, on a command whose whole
// job is moving a directory they cannot easily put back.
func TestRekeyDryRunChangesNothing(t *testing.T) {
	fx := newRekeyFixture(t)

	var out bytes.Buffer
	rekeyCmd.SetOut(&out)
	rekeyFlags.dryRun = true
	defer func() { rekeyFlags.dryRun = false }()
	if err := rekeyCmd.RunE(rekeyCmd, nil); err != nil {
		t.Fatalf("rekey --dry-run: %v", err)
	}

	oldBare, _ := store.BareDir(fx.oldFP)
	if _, err := os.Stat(oldBare); err != nil {
		t.Errorf("dry run moved the storage: %v", err)
	}
	newDir, _ := store.RepoDir(fx.newFP)
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Errorf("dry run created %s", newDir)
	}
	if !strings.Contains(out.String(), "--dry-run") {
		t.Errorf("dry run did not say it changed nothing:\n%s", out.String())
	}
}

// TestRekeyRefusesAmbiguousOwnership covers the one state rekey must not resolve on its own: moving
// the wrong storage dir is not something a later run can undo.
func TestRekeyRefusesAmbiguousOwnership(t *testing.T) {
	fx := newRekeyFixture(t)

	second := store.Meta{
		Fingerprint: "ffffffffffff",
		HostRoot:    fx.hostRoot,
		HostName:    "duplicate",
		Branch:      "repo/ffffffffffff",
		Mono:        true,
	}
	if err := store.SaveMeta(second); err != nil {
		t.Fatalf("save second meta: %v", err)
	}

	rekeyCmd.SetOut(new(bytes.Buffer))
	rekeyFlags.dryRun = false
	err := rekeyCmd.RunE(rekeyCmd, nil)
	if err == nil {
		t.Fatal("rekey accepted two overlays claiming one work tree")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %q, want it to name the ambiguity", err)
	}
	oldBare, _ := store.BareDir(fx.oldFP)
	if _, statErr := os.Stat(oldBare); statErr != nil {
		t.Errorf("refusal still moved storage: %v", statErr)
	}
}

// TestNoOverlayErrorNamesRekey is the diagnostic half of the fix. Before it, an orphaned overlay
// reported "run attic init", which starts an empty overlay beside a full one.
func TestNoOverlayErrorNamesRekey(t *testing.T) {
	fx := newRekeyFixture(t)

	hr, err := host.Detect(fx.hostRoot)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	msg := noOverlayError(hr).Error()
	for _, want := range []string{"attic rekey", fx.oldFP, fx.newFP} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	// The generic tail is the pre-fix advice; the orphan path must warn against init, not offer it.
	if strings.Contains(msg, "— run `attic init` or `attic clone") {
		t.Errorf("error still offers the generic init advice, which would strand the real history:\n%s", msg)
	}
	if !strings.Contains(msg, "Do NOT run `attic init`") {
		t.Errorf("error should warn against init explicitly:\n%s", msg)
	}
}

func TestRekeyNoopWhenAlreadyKeyed(t *testing.T) {
	newRekeyFixtureAlreadyKeyed(t)

	var out bytes.Buffer
	rekeyCmd.SetOut(&out)
	rekeyFlags.dryRun = false
	if err := rekeyCmd.RunE(rekeyCmd, nil); err != nil {
		t.Fatalf("rekey: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to do") {
		t.Errorf("expected a no-op report, got:\n%s", out.String())
	}
}

type rekeyFixture struct {
	hostRoot     string
	oldFP, newFP string
}

// newRekeyFixture builds a host repo with a mono overlay keyed to its original root commit, then
// rewrites that commit so the repo hashes to a new fingerprint — the orphaning a filter-repo run
// produces, reproduced with an amend.
func newRekeyFixture(t *testing.T) rekeyFixture {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	hostDir := t.TempDir()
	writeFile(t, hostDir, "README.md", "# host")
	writeFile(t, hostDir, ".gitignore", "# BEGIN attic\ndocs-internal\n# END attic\n")
	writeFile(t, hostDir, "docs-internal/a.md", "doc")
	git(t, hostDir, "init", "-q", ".")
	git(t, hostDir, "add", "-A")
	git(t, hostDir, "commit", "-qm", "init")

	hr, err := host.Detect(hostDir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	oldFP := hr.Fingerprint()
	buildMonoOverlay(t, hr.Root, oldFP)

	// Rewriting the sole commit rewrites the root commit, which is what the fingerprint is derived from.
	git(t, hostDir, "commit", "-q", "--amend", "-m", "rewritten")

	after, err := host.Detect(hostDir)
	if err != nil {
		t.Fatalf("detect after amend: %v", err)
	}
	t.Chdir(hostDir)
	return rekeyFixture{hostRoot: hr.Root, oldFP: oldFP, newFP: after.Fingerprint()}
}

// newRekeyFixtureAlreadyKeyed builds the same shape with no rewrite, so the overlay's key still
// matches the repo.
func newRekeyFixtureAlreadyKeyed(t *testing.T) rekeyFixture {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	hostDir := t.TempDir()
	writeFile(t, hostDir, "README.md", "# host")
	writeFile(t, hostDir, ".gitignore", "# BEGIN attic\ndocs-internal\n# END attic\n")
	writeFile(t, hostDir, "docs-internal/a.md", "doc")
	git(t, hostDir, "init", "-q", ".")
	git(t, hostDir, "add", "-A")
	git(t, hostDir, "commit", "-qm", "init")

	hr, err := host.Detect(hostDir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	fp := hr.Fingerprint()
	buildMonoOverlay(t, hr.Root, fp)
	t.Chdir(hostDir)
	return rekeyFixture{hostRoot: hr.Root, oldFP: fp, newFP: fp}
}

// buildMonoOverlay creates the storage a mono `attic init` would leave: a bare repo on repo/<fp> with
// one commit, wired to a remote, plus the meta that registers it to the host root.
func buildMonoOverlay(t *testing.T, hostRoot, fp string) {
	t.Helper()
	bare, err := store.BareDir(fp)
	if err != nil {
		t.Fatal(err)
	}
	branch := "repo/" + fp
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (gitwrap.Repo{}).Stream("init", "--bare", "-q", "-b", branch, bare); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if err := ensureOverlayExclude(bare); err != nil {
		t.Fatalf("ensureOverlayExclude: %v", err)
	}
	repo := gitwrap.Repo{GitDir: bare, WorkTree: hostRoot}
	if err := repo.Stream("remote", "add", "origin", "https://example.invalid/mono"); err != nil {
		t.Fatalf("remote add: %v", err)
	}
	if err := repo.Stream("config", "branch."+branch+".remote", "origin"); err != nil {
		t.Fatalf("branch remote: %v", err)
	}
	if err := repo.Stream("config", "branch."+branch+".merge", "refs/heads/"+branch); err != nil {
		t.Fatalf("branch merge: %v", err)
	}
	if err := repo.Stream("add", "--force", "--", "docs-internal"); err != nil {
		t.Fatalf("overlay add: %v", err)
	}
	if err := repo.Stream("commit", "-qm", "overlay"); err != nil {
		t.Fatalf("overlay commit: %v", err)
	}
	if err := store.SaveMeta(store.Meta{
		Fingerprint: fp,
		HostRoot:    hostRoot,
		HostName:    filepath.Base(hostRoot),
		Branch:      branch,
		Mono:        true,
		Remote:      "https://example.invalid/mono",
	}); err != nil {
		t.Fatalf("save meta: %v", err)
	}
}

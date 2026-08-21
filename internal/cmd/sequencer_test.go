package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
)

// TestSequencerIdleOnCleanOverlay pins the negative: the check must not fire on a healthy overlay,
// or every write path refuses forever.
func TestSequencerIdleOnCleanOverlay(t *testing.T) {
	_, repo := newOverlayFixture(t)
	seq, err := repo.Sequencer()
	if err != nil {
		t.Fatalf("Sequencer: %v", err)
	}
	if seq.InProgress() {
		t.Errorf("clean overlay reported mid-%s", seq.Op)
	}
	if err := ensureNoSequencer(repo, "sync"); err != nil {
		t.Errorf("ensureNoSequencer on clean overlay: %v", err)
	}
}

// TestSequencerDetectsStoppedRebase is the regression. `git status --porcelain` reports nothing once
// the conflict is staged, so attic read a wedged overlay as clean, started a second rebase, and died
// on git's "already a rebase-merge directory" pointing into its own private store.
func TestSequencerDetectsStoppedRebase(t *testing.T) {
	hr, repo := newOverlayFixture(t)
	wedgeOverlayRebase(t, hr, repo)

	seq, err := repo.Sequencer()
	if err != nil {
		t.Fatalf("Sequencer: %v", err)
	}
	if seq.Op != "rebase" {
		t.Fatalf("Op = %q, want \"rebase\"", seq.Op)
	}
	if seq.Abort != "rebase --abort" {
		t.Errorf("Abort = %q, want \"rebase --abort\"", seq.Abort)
	}

	// The exact shape of the loop: the conflict is staged, so porcelain is silent while the
	// sequencer state is still open.
	out, err := repo.Run("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Skipf("fixture left a dirty index (%q) — ensureCleanIndex would have caught this alone", out)
	}
	if err := ensureCleanIndex(repo); err != nil {
		t.Fatalf("ensureCleanIndex unexpectedly refused: %v", err)
	}
	if err := ensureNoSequencer(repo, "sync"); err == nil {
		t.Fatal("ensureNoSequencer accepted an overlay stopped mid-rebase")
	}
}

// TestSequencerErrNamesRunnableRecovery covers why attic refuses rather than letting git speak: git's
// own message offers `git rebase --continue` and an rm -fr of a path inside attic's data dir, neither
// of which reaches the overlay from the host repo.
func TestSequencerErrNamesRunnableRecovery(t *testing.T) {
	err := gitwrap.Sequencer{Op: "rebase", Abort: "rebase --abort"}.Err("sync")
	if err == nil {
		t.Fatal("Err returned nil for an in-progress sequencer")
	}
	for _, want := range []string{"sync:", "mid-rebase", "unresolved conflict", "attic exec rebase --continue", "attic exec rebase --abort"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Err() = %q, missing %q", err, want)
		}
	}
	if err := (gitwrap.Sequencer{}).Err("sync"); err != nil {
		t.Errorf("Err on idle sequencer = %v, want nil", err)
	}
}

// TestSequencerDistinguishesOperations checks each marker maps to the operation whose abort actually
// works: `git am` and `rebase --apply` share rebase-apply/, and offering the wrong --abort to a
// stopped am is a dead end.
func TestSequencerDistinguishesOperations(t *testing.T) {
	cases := []struct {
		marker, dir string
		op, abort   string
	}{
		{"rebase-apply/applying", "rebase-apply", "am", "am --abort"},
		{"rebase-merge/done", "rebase-merge", "rebase", "rebase --abort"},
		{"rebase-apply/next", "rebase-apply", "rebase", "rebase --abort"},
		{"CHERRY_PICK_HEAD", "", "cherry-pick", "cherry-pick --abort"},
		{"REVERT_HEAD", "", "revert", "revert --abort"},
		{"MERGE_HEAD", "", "merge", "merge --abort"},
		{"BISECT_LOG", "", "bisect", "bisect reset"},
	}
	for _, tc := range cases {
		t.Run(tc.op+"/"+tc.marker, func(t *testing.T) {
			bare := t.TempDir()
			if tc.dir != "" {
				if err := os.MkdirAll(filepath.Join(bare, tc.dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(bare, filepath.FromSlash(tc.marker)), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			seq, err := (gitwrap.Repo{GitDir: bare}).Sequencer()
			if err != nil {
				t.Fatalf("Sequencer: %v", err)
			}
			if seq.Op != tc.op || seq.Abort != tc.abort {
				t.Errorf("Sequencer = {%q, %q}, want {%q, %q}", seq.Op, seq.Abort, tc.op, tc.abort)
			}
		})
	}
}

// wedgeOverlayRebase reproduces the failure the way it happened: two histories append different text
// to the same file, the rebase stops on the conflict, and the snapshot hook's stage-then-commit marks
// it resolved and commits, leaving the sequencer state open behind a clean index.
func wedgeOverlayRebase(t *testing.T, hr host.Repo, repo gitwrap.Repo) {
	t.Helper()
	base, err := repo.Run("rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	base = strings.TrimSpace(base)

	stage := func() {
		if err := repo.Stream("add", "--force", "--", "docs-internal"); err != nil {
			t.Fatalf("overlay add: %v", err)
		}
	}
	commit := func(msg string) {
		if err := repo.Stream("commit", "-qm", msg); err != nil {
			t.Fatalf("overlay commit: %v", err)
		}
	}

	// The "other host" side, parked on a branch.
	writeFile(t, hr.Root, "docs-internal/log.md", "shared\nfrom host A\n")
	stage()
	commit("host A")
	theirs, err := repo.Run("rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if err := repo.Stream("branch", "-f", "theirs", strings.TrimSpace(theirs)); err != nil {
		t.Fatalf("branch theirs: %v", err)
	}

	// This host's side, from the same base.
	if err := repo.Stream("reset", "-q", "--hard", base); err != nil {
		t.Fatalf("reset: %v", err)
	}
	writeFile(t, hr.Root, "docs-internal/log.md", "shared\nfrom host B\n")
	stage()
	commit("host B")

	if err := repo.Stream("rebase", "theirs"); err == nil {
		t.Fatal("fixture did not conflict — the test proves nothing")
	}
	// What the snapshot hook did: re-stage the managed paths, then commit. git leaves rebase-merge/
	// in place, so the overlay is wedged while status reports a clean tree.
	stage()
	commit("overlay: snapshot")
}

// TestPassthroughGatesOnlyIntegrators pins the split: the reported symptom was `attic pull` dying on
// git's own "already a rebase-merge directory", while fetch, log and diff are how you find out why.
func TestPassthroughGatesOnlyIntegrators(t *testing.T) {
	gated := map[string]bool{}
	for _, p := range passthroughs {
		gated[p.use] = p.integrates
	}
	for use, want := range map[string]bool{"pull": true, "push": true, "fetch": false, "log": false, "diff": false} {
		if got, ok := gated[use]; !ok {
			t.Errorf("passthrough %q missing", use)
		} else if got != want {
			t.Errorf("passthrough %q integrates = %v, want %v", use, got, want)
		}
	}
}

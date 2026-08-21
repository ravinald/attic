package gitwrap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sequencer is the multi-step git operation an overlay is stopped in the middle of: a rebase, merge,
// cherry-pick, revert, am, or bisect. `git status --porcelain` reports none of them — the banner
// lives in the long format only — so a clean-index check reads a stopped rebase as a clean repo.
// Anything that stages, commits, or starts an integration has to ask for this separately.
type Sequencer struct {
	Op    string // "rebase", "merge", "cherry-pick", "revert", "am", "bisect"; empty when idle
	Abort string // the git subcommand that unwinds it, e.g. "rebase --abort"

	// Orphaned counts commits reachable only from HEAD, which --abort destroys for good: it resets
	// the branch to where the operation started, and a snapshot hook that keeps committing over a
	// wedge leaves its commits on no other ref. Zero for a fresh wedge, and zero once they are
	// reachable from a branch or a remote, which is exactly when --abort is safe again.
	Orphaned int
}

// InProgress reports whether git is stopped part-way through an operation.
func (s Sequencer) InProgress() bool { return s.Op != "" }

// Err returns the refusal to hand back to the caller, or nil when idle. It names the recovery in
// attic's own vocabulary: git's message points at `git rebase --continue` and, failing that, an
// `rm -fr` of a path inside attic's private store, neither of which a user can run as printed.
func (s Sequencer) Err(verb string) error {
	if !s.InProgress() {
		return nil
	}
	op := strings.SplitN(s.Abort, " ", 2)[0]
	msg := fmt.Sprintf("%s: overlay is mid-%s, so this would write over an unresolved conflict.\n"+
		"Finish it with `attic exec %s --continue`, or discard it with `attic exec %s`",
		verb, s.Op, op, s.Abort)
	if s.Orphaned > 0 {
		msg += fmt.Sprintf(".\n%d commit(s) are reachable only from HEAD, and `%s` would destroy them — "+
			"`attic exec %s --quit` closes the operation and keeps them (HEAD is left detached; "+
			"re-point the branch with `attic exec branch -f <branch> HEAD`)", s.Orphaned, s.Abort, op)
	}
	return errors.New(msg)
}

// sequencerMarkers maps a path inside the git dir to the operation its presence proves. Ordered
// most-specific first: a `git am` leaves rebase-apply/applying alongside the directory a
// `rebase --apply` leaves, and only the marker file separates them.
var sequencerMarkers = []struct{ path, op, abort string }{
	{filepath.Join("rebase-apply", "applying"), "am", "am --abort"},
	{"rebase-merge", "rebase", "rebase --abort"},
	{"rebase-apply", "rebase", "rebase --abort"},
	{"CHERRY_PICK_HEAD", "cherry-pick", "cherry-pick --abort"},
	{"REVERT_HEAD", "revert", "revert --abort"},
	{"MERGE_HEAD", "merge", "merge --abort"},
	{"BISECT_LOG", "bisect", "bisect reset"},
}

// Sequencer reports which multi-step operation, if any, the overlay is stopped in.
func (r Repo) Sequencer() (Sequencer, error) {
	dir := r.GitDir
	if dir == "" {
		out, err := r.Run("rev-parse", "--absolute-git-dir")
		if err != nil {
			return Sequencer{}, err
		}
		dir = strings.TrimSpace(out)
	}
	for _, m := range sequencerMarkers {
		if _, err := os.Lstat(filepath.Join(dir, m.path)); err == nil {
			return Sequencer{Op: m.op, Abort: m.abort, Orphaned: r.orphanedCommits()}, nil
		}
	}
	return Sequencer{}, nil
}

// orphanedCommits counts commits reachable from HEAD and from no ref at all. Counting against
// REBASE_HEAD instead over-reports, because the commits the operation had already replayed onto the
// new base are reachable from HEAD too and are not at risk. Advisory: it only decides which recovery
// command the message leads with, so any error here reports zero and the message stays correct.
func (r Repo) orphanedCommits() int {
	// Not `--not --all`: git documents --all as "all the refs in refs/, along with HEAD", so it
	// excludes the very tip being asked about and the count is always zero.
	out, err := r.cmd("rev-list", "--count", "HEAD", "--not", "--branches", "--tags", "--remotes").Output()
	if err != nil {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return 0
	}
	return n
}

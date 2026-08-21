package gitwrap

import (
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
	cont := strings.SplitN(s.Abort, " ", 2)[0] + " --continue"
	return fmt.Errorf("%s: overlay is mid-%s, so this would write over an unresolved conflict.\n"+
		"Finish it with `attic exec %s`, or discard it with `attic exec %s`",
		verb, s.Op, cont, s.Abort)
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
			return Sequencer{Op: m.op, Abort: m.abort}, nil
		}
	}
	return Sequencer{}, nil
}

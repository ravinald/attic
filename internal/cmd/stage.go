package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ravinald/attic/internal/ignore"
	"github.com/spf13/cobra"
)

var stageCmd = &cobra.Command{
	Use:   "stage [<path>...]",
	Short: "Stage new and modified files under paths the overlay already manages.",
	Long: `Re-stages what the overlay already tracks, without touching the host .gitignore block.

This is the counterpart to "attic add", which registers a path for the first time. Once a directory
is registered, new files beneath it reach the overlay index through stage, not through add: naming a
new file to add would append a redundant .gitignore line that ignores nothing new and misreports the
granularity the overlay is managed at.

With no arguments it stages every entry in the managed block, which is what a snapshot hook wants.
With arguments it stages only those paths, and refuses any the block does not already cover.`,
	RunE: func(_ *cobra.Command, args []string) error {
		hr, repo, err := openOverlay()
		if err != nil {
			return err
		}
		if err := ensureNoSequencer(repo, "stage"); err != nil {
			return err
		}
		blk, err := ignore.Load(gitignorePath(hr))
		if err != nil {
			return err
		}
		if len(blk.Lines) == 0 {
			return errors.New("stage: the managed .gitignore block is empty — register a path with `attic add <path>` first")
		}

		targets := blk.Lines
		if len(args) > 0 {
			rels, err := relativiseToHost(hr.Root, args)
			if err != nil {
				return err
			}
			if err := rejectUnregistered(blk, rels); err != nil {
				return err
			}
			targets = rels
		}

		// --force for the same reason `attic add` needs it: every managed path is gitignored upstream by
		// design, so a plain add would skip all of them.
		gitArgs := append([]string{"add", "--force", "--"}, targets...)
		if err := repo.Stream(gitArgs...); err != nil {
			return err
		}
		// A path can sit in the block while the host index also tracks it. Force-staging that forks one
		// file into two histories that silently diverge, so the host's claim wins.
		return ejectFromHost(hr, targets)
	},
}

// rejectUnregistered fails unless the block already covers every path, naming the command that would
// register one. Staging an unregistered path would put it in the overlay index while the host still
// ignores nothing on its behalf, so the next `git add -A` carries it upstream.
func rejectUnregistered(blk ignore.Block, rels []string) error {
	var unknown []string
	for _, r := range rels {
		if blk.Has(r) {
			continue
		}
		if _, ok := blk.Covers(r); ok {
			continue
		}
		unknown = append(unknown, r)
	}
	if len(unknown) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("stage: not managed by the overlay:")
	for _, u := range unknown {
		b.WriteString("\n  " + u)
	}
	fmt.Fprintf(&b, "\nregister with `attic add %s` — stage only re-stages paths already in the managed block", unknown[0])
	return errors.New(b.String())
}

func init() {
	root.AddCommand(stageCmd)
}

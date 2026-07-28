package cmd

import (
	"fmt"
	"os"

	"github.com/ravinald/attic/internal/ignore"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var addFlags struct {
	onDuplicate string
}

var addCmd = &cobra.Command{
	Use:   "add <path>...",
	Short: "Stage paths in the overlay and add them to the host .gitignore block.",
	Long:  "Each path is normalised relative to the host repo root, written into the attic-managed block in the host's .gitignore, and staged with `git add --force` (the force is intentional: the path is gitignored upstream by design).",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		hr, repo, err := openOverlay()
		if err != nil {
			return err
		}
		rels, err := relativiseToHost(hr.Root, args)
		if err != nil {
			return err
		}

		// Update the .gitignore block first; if that fails, no overlay state changed.
		blk, err := ignore.Load(gitignorePath(hr))
		if err != nil {
			return err
		}
		mode, err := resolveOnDuplicate(addFlags.onDuplicate)
		if err != nil {
			return err
		}
		scope := duplicateScope(mode, blk, rels)
		blk.Add(rels...)

		var dups []ignore.Duplicate
		if len(scope) > 0 {
			if dups, err = ignore.FindDuplicates(gitignorePath(hr), scope); err != nil {
				return err
			}
			if mode == store.OnDuplicateManage {
				for _, d := range dups {
					blk.DropOutside(d.Text)
				}
			}
		}
		if err := blk.Save(); err != nil {
			return err
		}
		reportDuplicates(mode, dups)

		// Stage in the overlay. --force is required because the paths are now gitignored.
		gitArgs := append([]string{"add", "--force", "--"}, rels...)
		if err := repo.Stream(gitArgs...); err != nil {
			return err
		}

		// The overlay owns these paths now; evict any copy the host index still tracks so a
		// prior force-add or a pre-ignore `git add -A` can't carry them into host history.
		if err := ejectFromHost(hr, rels); err != nil {
			return err
		}
		return nil
	},
}

// duplicateScope returns the paths to scan for redundant rules outside the block. It must be called
// before Block.Add, which erases the distinction it depends on.
//
// Warn mode sees only the paths this run adopts. Its diagnostic announces a state change — the block
// has just begun shadowing an outside rule — and re-adding a settled path changes nothing, so an
// unscoped warn nags on every `attic add` of an established directory. Manage mode still scans
// everything: deleting the outside rule is self-limiting (a later run finds nothing to remove), and
// that keeps a switch to manage able to absorb a rule an earlier off/warn run left in place.
func duplicateScope(mode string, blk ignore.Block, rels []string) []string {
	switch mode {
	case store.OnDuplicateOff:
		return nil
	case store.OnDuplicateWarn:
		var fresh []string
		for _, r := range rels {
			if !blk.Has(r) {
				fresh = append(fresh, r)
			}
		}
		return fresh
	default:
		return rels
	}
}

// reportDuplicates prints one diagnostic per redundant outside rule. Manage mode has already dropped
// them via the block Save; warn mode leaves them and nudges toward manage. Off never gets here.
func reportDuplicates(mode string, dups []ignore.Duplicate) {
	for _, d := range dups {
		if mode == store.OnDuplicateManage {
			fmt.Fprintf(os.Stderr, "attic: removed redundant .gitignore:%d %q — the managed block owns %q now\n",
				d.Line, d.Text, d.Path)
			continue
		}
		fmt.Fprintf(os.Stderr, "attic: %q is already ignored by .gitignore:%d %q; the managed block now also covers it "+
			"(set gitignore.on_duplicate=manage to absorb it)\n", d.Path, d.Line, d.Text)
	}
}

func init() {
	addCmd.Flags().StringVar(&addFlags.onDuplicate, "on-duplicate", "",
		"Override the on_duplicate policy for this run: off | warn | manage.")
	root.AddCommand(addCmd)
}

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
		blk.Add(rels...)

		mode, err := resolveOnDuplicate(addFlags.onDuplicate)
		if err != nil {
			return err
		}
		var dups []ignore.Duplicate
		if mode != store.OnDuplicateOff {
			if dups, err = ignore.FindDuplicates(gitignorePath(hr), rels); err != nil {
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

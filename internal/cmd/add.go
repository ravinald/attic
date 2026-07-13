package cmd

import (
	"github.com/ravinald/attic/internal/ignore"
	"github.com/spf13/cobra"
)

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
		if err := blk.Save(); err != nil {
			return err
		}

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

func init() {
	root.AddCommand(addCmd)
}

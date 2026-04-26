package cmd

import (
	"github.com/ravinald/attic/internal/ignore"
	"github.com/spf13/cobra"
)

var rmFlags struct {
	delete bool
}

var rmCmd = &cobra.Command{
	Use:   "rm <path>...",
	Short: "Stop tracking paths in the overlay and remove them from the host .gitignore block.",
	Long:  "By default the file stays on disk (only the index entry and the .gitignore line are removed). Pass --delete to also remove the file from the work tree.",
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

		gitArgs := []string{"rm", "-r"}
		if !rmFlags.delete {
			gitArgs = append(gitArgs, "--cached")
		}
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, rels...)
		if err := repo.Stream(gitArgs...); err != nil {
			return err
		}

		blk, err := ignore.Load(gitignorePath(hr))
		if err != nil {
			return err
		}
		blk.Remove(rels...)
		if err := blk.Save(); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rmCmd.Flags().BoolVar(&rmFlags.delete, "delete", false, "Also delete the file from the work tree.")
	root.AddCommand(rmCmd)
}

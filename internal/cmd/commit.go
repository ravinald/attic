package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

var commitFlags struct {
	message    string
	all        bool
	allowEmpty bool
}

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit staged overlay changes.",
	RunE: func(_ *cobra.Command, args []string) error {
		_, repo, err := openOverlay()
		if err != nil {
			return err
		}
		// An overlay is scratch history — a message rarely earns its keep. Without -m, git would
		// drop into an editor that aborts on an empty message; synthesise a timestamped one instead.
		msg := commitFlags.message
		if msg == "" {
			msg = "attic snapshot " + time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
		}
		gitArgs := []string{"commit", "-m", msg}
		if commitFlags.all {
			gitArgs = append(gitArgs, "-a")
		}
		if commitFlags.allowEmpty {
			gitArgs = append(gitArgs, "--allow-empty")
		}
		gitArgs = append(gitArgs, args...)
		return repo.Stream(gitArgs...)
	},
}

func init() {
	commitCmd.Flags().StringVarP(&commitFlags.message, "message", "m", "", "Commit message.")
	commitCmd.Flags().BoolVarP(&commitFlags.all, "all", "a", false, "Stage modifications to tracked files before committing.")
	commitCmd.Flags().BoolVar(&commitFlags.allowEmpty, "allow-empty", false, "Allow an empty commit.")
	root.AddCommand(commitCmd)
}

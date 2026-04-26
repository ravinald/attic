package cmd

import "github.com/spf13/cobra"

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
		gitArgs := []string{"commit"}
		if commitFlags.message != "" {
			gitArgs = append(gitArgs, "-m", commitFlags.message)
		}
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

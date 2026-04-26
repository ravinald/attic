package cmd

import "github.com/spf13/cobra"

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List paths tracked in the overlay.",
	RunE: func(_ *cobra.Command, _ []string) error {
		_, repo, err := openOverlay()
		if err != nil {
			return err
		}
		return repo.Stream("ls-files")
	},
}

func init() {
	root.AddCommand(lsCmd)
}

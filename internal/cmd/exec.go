package cmd

import "github.com/spf13/cobra"

var execCmd = &cobra.Command{
	Use:                "exec -- <git-args>",
	Short:              "Run an arbitrary git command against the overlay (escape hatch).",
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		_, repo, err := openOverlay()
		if err != nil {
			return err
		}
		return repo.Stream(args...)
	},
}

func init() {
	root.AddCommand(execCmd)
}

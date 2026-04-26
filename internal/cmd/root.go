// Package cmd wires the attic CLI together.
package cmd

import "github.com/spf13/cobra"

var root = &cobra.Command{
	Use:           "attic",
	Short:         "Track files alongside a git repo without committing them.",
	Long:          "attic keeps a per-host-repo bare git overlay. Files live in the host work tree, history lives outside it, and a marker block in the host .gitignore stops them leaking upstream.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error {
	return root.Execute()
}

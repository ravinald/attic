package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata, set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version, commit, build date.",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Printf("attic %s\n  commit: %s\n  built:  %s\n  go:     %s %s/%s\n",
			version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil
	},
}

func init() {
	root.AddCommand(versionCmd)
}

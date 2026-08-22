package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"

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
		v, c, d := buildInfo()
		fmt.Printf("attic %s\n  commit: %s\n  built:  %s\n  go:     %s %s/%s\n",
			v, c, d, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil
	},
}

// buildInfo resolves the build stamp, preferring the -ldflags values a release build bakes in.
//
// The README's install line is `go install`, which passes no ldflags at all, so without a fallback
// every binary a user installs reports itself as "dev". The toolchain records what that build does
// know: the module version for a `pkg@version` install, and the VCS revision and time for a build
// from a checkout. Neither source carries both, hence the per-field fallback.
func buildInfo() (v, c, d string) {
	v, c, d = version, commit, date
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	if v == "dev" && bi.Main.Version != "" {
		v = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision" && c == "none":
			c = s.Value
		case s.Key == "vcs.time" && d == "unknown":
			d = s.Value
		}
	}
	return v, c, d
}

func init() {
	root.AddCommand(versionCmd)
}

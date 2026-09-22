// Package cmd wires the attic CLI together.
package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

// ExitOrphaned is the exit status of any overlay command run in a work tree whose overlay is filed
// under a stale fingerprint. A hook gating on `attic status` otherwise reads this as "no overlay
// here" and goes quiet, which is the one reading that stops history being recorded with no error.
const ExitOrphaned = 4

var root = &cobra.Command{
	Use:   "attic",
	Short: "Track files alongside a git repo without committing them.",
	Long: `attic keeps a per-host-repo bare git overlay. Files live in the host work tree, history lives outside it, and a marker block in the host .gitignore stops them leaking upstream.

Exit status:
  0  success
  1  any error, including "no overlay for this repo"
  4  an overlay exists for this work tree under a stale fingerprint (the host history was rewritten); run ` + "`attic rekey`",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// exitError carries a non-default exit status out through cobra, which only returns an error.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

// Execute runs the root command.
func Execute() error {
	return root.Execute()
}

// ExitCode maps an error returned by Execute to the process exit status.
func ExitCode(err error) int {
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

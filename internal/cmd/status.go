package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:                "status",
	Short:              "Show overlay status (git status), plus overlay files git can't see.",
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		hr, repo, err := openOverlay()
		if err != nil {
			return err
		}
		if err := repo.Stream(append([]string{"status"}, args...)...); err != nil {
			return err
		}
		if machineReadable(args) {
			return nil
		}
		scope, err := overlayScope(hr, repo)
		if err != nil {
			return err
		}
		untracked, err := untrackedOverlayFiles(repo, scope)
		if err != nil {
			return err
		}
		if len(untracked) == 0 {
			return nil
		}
		// git printed its verdict above without these files in view — the host .gitignore hides them
		// from it — so the header has to explain why "working tree clean" and this list coexist.
		fmt.Println()
		fmt.Println("Untracked overlay files (hidden from git by the host .gitignore):")
		fmt.Println("  (use \"attic add <file>...\" to include in what will be committed)")
		fmt.Println()
		for _, f := range untracked {
			fmt.Printf("\t%s\n", f)
		}
		fmt.Println()
		return nil
	},
}

// machineReadable reports whether the caller asked for output another program will parse. The extra
// section is prose, so it must not land in a --porcelain stream someone is piping into a script.
func machineReadable(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-s", a == "--short", a == "-z":
			return true
		case strings.HasPrefix(a, "--porcelain"):
			return true
		case a == "--": // everything after is a pathspec
			return false
		}
	}
	return false
}

func init() {
	root.AddCommand(statusCmd)
}

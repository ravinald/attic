package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/ignore"
	"github.com/spf13/cobra"
)

var ejectFlags struct {
	check bool
}

var ejectCmd = &cobra.Command{
	Use:   "eject",
	Short: "Remove attic-managed paths from the HOST git index (not the overlay or disk).",
	Long: `Evicts every attic-managed path from the host repo's index so it stops being tracked or staged upstream.

Working-tree files and overlay history are untouched. Run it to clean a repo where an overlay path was force-added or committed to the host before attic adopted it.

--check makes no changes: it exits non-zero when a managed path is staged as an addition in the host index. Suitable for a pre-commit guard.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		hr, repo, err := openOverlay()
		if err != nil {
			return err
		}
		managed, err := managedPaths(hr, repo)
		if err != nil {
			return err
		}
		if len(managed) == 0 {
			fmt.Println("attic: no managed paths to eject")
			return nil
		}

		if ejectFlags.check {
			args := append([]string{"diff", "--cached", "--name-only", "--diff-filter=A", "--"}, managed...)
			out, err := hostGit(hr.Root, args...)
			if err != nil {
				return err
			}
			if s := strings.TrimSpace(out); s != "" {
				return fmt.Errorf("attic-managed paths staged in host index:\n%s", s)
			}
			return nil
		}

		if err := ejectFromHost(hr, managed); err != nil {
			return err
		}
		fmt.Printf("attic: ejected %d managed path(s) from the host index: %s\n", len(managed), strings.Join(managed, ", "))
		return nil
	},
}

// managedPaths is the authoritative set of host paths the overlay owns: the union of the
// .gitignore block entries and the top-level segments of the overlay's tracked files. The
// union covers a hand-cleared ignore block whose files still sit in the host index. The
// overlay side reads ls-files (the index), not ls-tree HEAD, so it holds before the first
// snapshot commit exists.
func managedPaths(hr host.Repo, repo gitwrap.Repo) ([]string, error) {
	set := make(map[string]struct{})
	blk, err := ignore.Load(gitignorePath(hr))
	if err != nil {
		return nil, err
	}
	for _, l := range blk.Lines {
		set[strings.TrimSuffix(l, "/")] = struct{}{}
	}
	out, err := repo.Run("ls-files")
	if err != nil {
		return nil, err
	}
	for _, top := range topLevels(strings.Split(strings.TrimSpace(out), "\n")) {
		set[top] = struct{}{}
	}

	managed := make([]string, 0, len(set))
	for p := range set {
		if p != "" {
			managed = append(managed, p)
		}
	}
	sort.Strings(managed)
	return managed, nil
}

func init() {
	ejectCmd.Flags().BoolVar(&ejectFlags.check, "check", false, "Report (exit non-zero) if a managed path is staged in the host index; make no changes.")
	root.AddCommand(ejectCmd)
}

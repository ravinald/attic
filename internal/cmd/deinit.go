package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/ignore"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var deinitFlags struct {
	force bool
}

var deinitCmd = &cobra.Command{
	Use:   "deinit",
	Short: "Remove the current overlay's local storage and .gitignore block (files on disk stay).",
	Long: `Undoes 'attic init'/'attic clone' for the current host repo: deletes the bare overlay and its
meta under $XDG_DATA_HOME/attic, and strips attic's block from the host .gitignore.

Work-tree files are never touched — deinit forgets how to track them, it doesn't delete them. It
refuses when the overlay holds commits not on its remote (they'd be lost); --force overrides.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		fp := hr.Fingerprint()
		repoDir, err := store.RepoDir(fp)
		if err != nil {
			return err
		}
		if _, err := os.Stat(repoDir); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("deinit: no overlay for %s — nothing to remove", hr.Root)
			}
			return fmt.Errorf("deinit: stat %s: %w", repoDir, err)
		}

		bare, err := store.BareDir(fp)
		if err != nil {
			return err
		}
		if !deinitFlags.force {
			if reason, unpushed := overlayUnpushed(gitwrap.Repo{GitDir: bare, WorkTree: hr.Root}); unpushed {
				return fmt.Errorf("deinit: overlay has %s — push first or pass --force to discard", reason)
			}
		}

		blk, err := ignore.Load(gitignorePath(hr))
		if err != nil {
			return err
		}
		hadBlock := len(blk.Lines) > 0
		blk.Lines = nil
		if err := blk.Save(); err != nil {
			return err
		}

		if err := os.RemoveAll(repoDir); err != nil {
			return fmt.Errorf("deinit: remove %s: %w", repoDir, err)
		}

		fmt.Printf("attic: removed overlay %s for %s\n  bare + meta deleted; work-tree files untouched\n", fp, hr.Root)
		if hadBlock {
			fmt.Println("  cleared the attic block from .gitignore — those paths are no longer ignored")
		}
		return nil
	},
}

// overlayUnpushed reports whether the overlay holds commits that only exist locally — either commits
// with no upstream, or commits ahead of the upstream ref. An unborn branch (a fresh init with
// nothing committed) has nothing to lose. reason is a human phrase for the refusal message.
func overlayUnpushed(repo gitwrap.Repo) (reason string, unpushed bool) {
	if _, err := gitQuiet(repo, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", false // unborn branch: no commits
	}
	if _, err := gitQuiet(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		return "local commits with no remote", true
	}
	out, err := gitQuiet(repo, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return "commits that can't be compared with its remote", true
	}
	if n := strings.TrimSpace(out); n != "0" {
		return n + " commit(s) not on the remote", true
	}
	return "", false
}

func init() {
	deinitCmd.Flags().BoolVar(&deinitFlags.force, "force", false, "Remove the overlay even if it holds commits not on its remote.")
	root.AddCommand(deinitCmd)
}

package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/spf13/cobra"
)

var syncFlags struct {
	strategy string
}

// syncResult summarises what attic sync did so the user gets one terse closing line after git's own output.
type syncResult struct {
	Strategy string
	Pulled   int
	Pushed   int
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Fetch + rebase|merge + push the overlay against its remote.",
	Long: `Day-to-day multi-machine loop: pulls remote commits, integrates them, pushes local ones.

Default strategy is rebase, which keeps repo/<fp> linear. --strategy=merge does a fast-forward-only merge instead (use 'attic exec -- merge' if you actually want a non-FF merge commit).

On the very first sync after init, no upstream exists yet — sync still works: it fetches, sees no remote branch, and pushes (push.autoSetupRemote sets up tracking on the way out).

Refuses to run with a dirty index or modifications to overlay-tracked files. Untracked host files are ignored.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		_, repo, err := openOverlay()
		if err != nil {
			return err
		}
		if err := ensureNoSequencer(repo, "sync"); err != nil {
			return err
		}
		if err := ensureCleanIndex(repo); err != nil {
			return err
		}
		if _, err := repo.Run("remote", "get-url", "origin"); err != nil {
			return errors.New("sync: overlay has no `origin` remote — run `attic init --mono-remote` or `attic init --remote` first")
		}

		strategy := strings.ToLower(syncFlags.strategy)
		switch strategy {
		case "rebase", "merge":
		default:
			return fmt.Errorf("sync: unknown strategy %q (use rebase or merge)", strategy)
		}

		branch, err := repo.Run("symbolic-ref", "--short", "HEAD")
		if err != nil {
			return fmt.Errorf("sync: read current branch: %w", err)
		}
		branch = strings.TrimSpace(branch)
		if branch == "" {
			return errors.New("sync: overlay HEAD is detached — checkout a branch first")
		}
		if err := ensureMonoFetch(repo, branch); err != nil {
			return err
		}

		beforeLocal := mustRevParse(repo, "HEAD")
		beforeRemote := tryRevParse(repo, "refs/remotes/origin/"+branch)

		remoteHas, err := remoteHasBranch(repo, "origin", branch)
		if err != nil {
			return err
		}
		if remoteHas {
			if err := repo.Stream("fetch", "origin", branch); err != nil {
				return err
			}
		}

		afterRemote := tryRevParse(repo, "refs/remotes/origin/"+branch)
		pulled := 0
		if beforeRemote != afterRemote && afterRemote != "" {
			rng := beforeRemote + ".." + afterRemote
			if beforeRemote == "" {
				rng = afterRemote
			}
			pulled = countCommits(repo, rng)
		}

		// Only integrate if the remote branch actually exists.
		if afterRemote != "" {
			switch strategy {
			case "rebase":
				if err := repo.Stream("rebase", "refs/remotes/origin/"+branch); err != nil {
					return err
				}
			case "merge":
				if err := repo.Stream("merge", "--ff-only", "refs/remotes/origin/"+branch); err != nil {
					return err
				}
			}
		}

		afterLocal := mustRevParse(repo, "HEAD")
		pushed := 0
		switch {
		case afterRemote == "" && afterLocal != "":
			// Branch doesn't exist remotely yet — every local commit is a push.
			pushed = countCommits(repo, afterLocal)
		case afterRemote != "" && afterLocal != afterRemote:
			pushed = countCommits(repo, afterRemote+".."+afterLocal)
		}

		if pushed > 0 || (afterRemote == "" && afterLocal != "") {
			if err := repo.Stream("push"); err != nil {
				return err
			}
		}
		_ = beforeLocal
		printSyncResult(syncResult{Strategy: strategy, Pulled: pulled, Pushed: pushed})
		return nil
	},
}

// ensureCleanIndex refuses to sync when the overlay has staged-but-uncommitted changes
// or modifications to tracked files. Untracked files in the host work tree are intentionally
// ignored — the overlay only owns the paths it tracks.
func ensureCleanIndex(repo gitwrap.Repo) error {
	out, err := repo.Run("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return errors.New("sync: overlay has uncommitted changes to tracked files — commit or stash first")
	}
	return nil
}

func mustRevParse(repo gitwrap.Repo, ref string) string {
	out, err := repo.Run("rev-parse", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// tryRevParse resolves a ref without leaking git's "unknown revision" stderr — used to probe
// whether a remote tracking branch exists.
func tryRevParse(repo gitwrap.Repo, ref string) string {
	out, err := gitQuiet(repo, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// remoteHasBranch checks whether <remote> publishes <branch> via ls-remote.
// Returning (false, nil) on absence keeps sync's first-push path silent.
func remoteHasBranch(repo gitwrap.Repo, remote, branch string) (bool, error) {
	out, err := repo.Run("ls-remote", "--heads", remote, branch)
	if err != nil {
		return false, fmt.Errorf("sync: ls-remote %s: %w", remote, err)
	}
	return strings.TrimSpace(out) != "", nil
}

func countCommits(repo gitwrap.Repo, rng string) int {
	out, err := repo.Run("rev-list", "--count", rng)
	if err != nil {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n
}

func printSyncResult(r syncResult) {
	switch {
	case r.Pulled == 0 && r.Pushed == 0:
		fmt.Println("attic: already in sync")
	default:
		fmt.Printf("attic: %s — pulled %d, pushed %d\n", r.Strategy, r.Pulled, r.Pushed)
	}
}

func init() {
	syncCmd.Flags().StringVar(&syncFlags.strategy, "strategy", "rebase", "Integration strategy: rebase (default) or merge (fast-forward only).")
	root.AddCommand(syncCmd)
}

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var rekeyFlags struct {
	dryRun bool
}

var rekeyCmd = &cobra.Command{
	Use:   "rekey",
	Short: "Re-point an overlay orphaned by a host history rewrite onto the repo's current fingerprint.",
	Long: `attic keys overlay storage by the host repo's root commit, so anything that rewrites that commit
— git filter-repo, filter-branch, a squashed or grafted root — moves the key and leaves the overlay
unreachable. Every other attic command then reports "no overlay for <path>" even though the history
is intact on disk and on the remote.

rekey finds the storage registered to this work tree under the old fingerprint and re-points it:
moves the storage dir, renames the mono-remote branch, rewrites the branch config and fetch refspec,
and updates meta.toml. Overlay history is never rewritten, only re-labelled.

The old branch is left on the mono remote. Publish the new one with "attic push" when ready; the
stale branch costs nothing and is a fallback until you delete it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		newFP := hr.Fingerprint()

		bare, err := store.BareDir(newFP)
		if err != nil {
			return err
		}
		if _, err := os.Stat(bare); err == nil {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "attic: overlay already keyed to %s — nothing to do\n", newFP)
			return nil
		}

		old, err := soleOrphan(hr, newFP)
		if err != nil {
			return err
		}

		oldDir, err := store.RepoDir(old.Fingerprint)
		if err != nil {
			return err
		}
		newDir, err := store.RepoDir(newFP)
		if err != nil {
			return err
		}
		if _, err := os.Stat(newDir); err == nil {
			return fmt.Errorf("rekey: %s already exists but holds no attic.git — resolve it by hand before re-keying", newDir)
		}

		newBranch := old.Branch
		if isMonoBranch(old.Branch) {
			newBranch = overlayBranch(newFP)
		}

		if rekeyFlags.dryRun {
			printRekeyPlan(cmd.OutOrStdout(), old, newFP, oldDir, newDir, newBranch)
			return nil
		}

		if err := os.Rename(oldDir, newDir); err != nil {
			return fmt.Errorf("rekey: move %s -> %s: %w", oldDir, newDir, err)
		}
		// Past this point the storage already lives under the new key, so any failure must put it back
		// or the overlay is reachable under neither fingerprint.
		restore := func(cause error) error {
			if err := os.Rename(newDir, oldDir); err != nil {
				return fmt.Errorf("%w (rollback also failed, storage left at %s: %v)", cause, newDir, err)
			}
			return cause
		}

		if newBranch != old.Branch {
			repo := gitwrap.Repo{GitDir: filepath.Join(newDir, "attic.git"), WorkTree: hr.Root}
			if err := renameOverlayBranch(repo, old.Branch, newBranch); err != nil {
				return restore(err)
			}
		}

		m := old
		m.Fingerprint = newFP
		m.Branch = newBranch
		if err := store.SaveMeta(m); err != nil {
			return restore(fmt.Errorf("rekey: save meta: %w", err))
		}

		w := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(w, "attic: re-keyed overlay %s -> %s\n", old.Fingerprint, newFP)
		_, _ = fmt.Fprintf(w, "  storage  %s\n", newDir)
		if newBranch != old.Branch {
			_, _ = fmt.Fprintf(w, "  branch   %s -> %s\n", old.Branch, newBranch)
			_, _ = fmt.Fprintf(w, "attic: the mono remote still has %s; publish the new branch with `attic push`\n", old.Branch)
		}
		return nil
	},
}

// soleOrphan returns the single overlay registered to this work tree under a stale fingerprint. It
// refuses ambiguity rather than picking one: two overlays claiming one work tree is a state a person
// has to look at, and moving the wrong storage dir is not something a later run can undo.
func soleOrphan(hr host.Repo, newFP string) (store.Meta, error) {
	orphans, err := store.FindMetasByHostRoot(hr.Root)
	if err != nil {
		return store.Meta{}, err
	}
	switch len(orphans) {
	case 0:
		return store.Meta{}, fmt.Errorf("rekey: no overlay storage is registered to %s — nothing to re-point; run `attic init` or `attic clone <remote>` to start one", hr.Root)
	case 1:
		if orphans[0].Fingerprint == newFP {
			return store.Meta{}, fmt.Errorf("rekey: meta for %s already reads %s but its attic.git is missing — restore the bare repo or re-clone with `attic clone`", hr.Root, newFP)
		}
		return orphans[0], nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "rekey: %d overlays claim %s, so which one to re-point is ambiguous:", len(orphans), hr.Root)
		for _, m := range orphans {
			fmt.Fprintf(&b, "\n  %s  %s", m.Fingerprint, m.DisplayLabel())
		}
		b.WriteString("\nresolve by hand: keep one and `attic deinit` or delete the rest")
		return store.Meta{}, errors.New(b.String())
	}
}

// renameOverlayBranch moves the overlay's local branch and re-wires everything keyed to its old name.
// The stale remote-tracking ref goes too: left behind, `attic status` reports the overlay ahead of a
// branch that no longer corresponds to it.
func renameOverlayBranch(repo gitwrap.Repo, oldBranch, newBranch string) error {
	if err := repo.Stream("branch", "-m", oldBranch, newBranch); err != nil {
		return fmt.Errorf("rekey: rename branch %s -> %s: %w", oldBranch, newBranch, err)
	}
	// An overlay wired before a given attic version may lack either key, so a missing one is not an error.
	_, _ = repo.Run("config", "--unset-all", "branch."+oldBranch+".remote")
	_, _ = repo.Run("config", "--unset-all", "branch."+oldBranch+".merge")
	if err := repo.Stream("config", "branch."+newBranch+".remote", "origin"); err != nil {
		return fmt.Errorf("rekey: set branch remote: %w", err)
	}
	if err := repo.Stream("config", "branch."+newBranch+".merge", "refs/heads/"+newBranch); err != nil {
		return fmt.Errorf("rekey: set branch merge: %w", err)
	}
	if err := ensureMonoFetch(repo, newBranch); err != nil {
		return err
	}
	_, _ = repo.Run("update-ref", "-d", "refs/remotes/origin/"+oldBranch)
	return nil
}

// isMonoBranch reports whether a branch name is a mono-remote overlay branch, whose name carries the
// fingerprint and therefore has to change with it. A per-repo overlay owns its whole remote and sits
// on "main", which no re-key affects.
func isMonoBranch(branch string) bool {
	return strings.HasPrefix(branch, overlayBranchPrefix)
}

func printRekeyPlan(w io.Writer, old store.Meta, newFP, oldDir, newDir, newBranch string) {
	_, _ = fmt.Fprintf(w, "attic: would re-key overlay %s -> %s\n", old.Fingerprint, newFP)
	_, _ = fmt.Fprintf(w, "  storage  %s\n        -> %s\n", oldDir, newDir)
	if newBranch != old.Branch {
		_, _ = fmt.Fprintf(w, "  branch   %s -> %s\n", old.Branch, newBranch)
	}
	_, _ = fmt.Fprintln(w, "attic: nothing changed (--dry-run)")
}

// noOverlayError explains a missing overlay. When storage is registered to this very work tree under
// a different fingerprint the overlay is orphaned rather than absent, and the generic "run attic
// init" advice is actively harmful there: init would start an empty overlay beside a full one and the
// real history would stay unreachable.
func noOverlayError(hr host.Repo) error {
	orphans, err := store.FindMetasByHostRoot(hr.Root)
	if err != nil || len(orphans) == 0 {
		return fmt.Errorf("no overlay for %s — run `attic init` or `attic clone <remote>`", hr.Root)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "no overlay for %s at fingerprint %s", hr.Root, hr.Fingerprint())
	for _, m := range orphans {
		fmt.Fprintf(&b, "\n  storage exists under fingerprint %s (%s)", m.Fingerprint, m.DisplayLabel())
	}
	b.WriteString("\nattic keys overlays by the host repo's root commit, so rewriting history (filter-repo, filter-branch, a squashed or grafted root) moves the key and orphans the overlay.")
	b.WriteString("\nre-point it with `attic rekey`. Do NOT run `attic init`: that starts an empty overlay beside the existing one and leaves the real history unreachable.")
	return errors.New(b.String())
}

func init() {
	rekeyCmd.Flags().BoolVar(&rekeyFlags.dryRun, "dry-run", false, "Print what would move and change nothing.")
	root.AddCommand(rekeyCmd)
}

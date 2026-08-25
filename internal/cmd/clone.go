package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/ignore"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var cloneFlags struct {
	force bool
	mono  bool
}

var cloneCmd = &cobra.Command{
	Use:   "clone [remote]",
	Short: "Restore an existing overlay into the current host repo from its remote.",
	Long: `Clones the bare overlay locally and checks out tracked paths into the host work tree.

For a per-host-repo remote (default): clones the whole bare from <remote>.
For a shared mono remote (--mono): clones only the repo/<fp> branch from <remote>, where <fp> is the host repo's fingerprint. With --mono the remote may be omitted when this machine already has exactly one mono remote.

Refuses to clobber existing files unless --force, and refuses paths the host repo tracks either way.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		var remote string
		switch {
		case len(args) == 1:
			remote = args[0]
		case cloneFlags.mono:
			r, err := soleMonoRemote()
			if err != nil {
				return fmt.Errorf("clone: %w", err)
			}
			remote = r
		default:
			return fmt.Errorf("clone: a remote URL is required (or use --mono to default to this machine's mono remote)")
		}
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		fp := hr.Fingerprint()
		bare, err := store.BareDir(fp)
		if err != nil {
			return err
		}
		if _, err := os.Stat(bare); err == nil {
			return fmt.Errorf("clone: overlay already exists at %s — remove it or use the existing one", bare)
		}
		// A mono remote carries one orphan branch per fingerprint plus the label map, and its HEAD points
		// at none of this repo's files. Cloning it whole drags every project's history in and checks the
		// label branch out over the host work tree, so name the flag rather than guess the mode.
		if !cloneFlags.mono {
			mono, err := remoteLooksMono(remote)
			if err != nil {
				return err
			}
			if mono {
				return fmt.Errorf("clone: %s is a shared mono remote — pass --mono to take branch %s from it",
					remote, overlayBranch(fp))
			}
		}
		if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
			return fmt.Errorf("clone: mkdir parent: %w", err)
		}
		// Anything that fails from here leaves a bare nothing has finished provisioning, and the next
		// attempt then dies on "overlay already exists" instead of the real error. Roll it back so one
		// wrong flag stays one wrong flag. The early return above means this never touches an existing
		// overlay, and meta is written last, so a rolled-back clone leaves no record either.
		provisioned := false
		defer func() {
			if !provisioned {
				_ = os.RemoveAll(bare)
			}
		}()

		branch := "main"
		if cloneFlags.mono {
			branch = overlayBranch(fp)
			// Pre-flight: confirm the branch exists on the mono remote so the user gets a useful error.
			has, err := remoteHasBranch(gitwrap.Repo{}, remote, branch)
			if err != nil {
				return err
			}
			if !has {
				return fmt.Errorf("clone: no overlay branch %s on %s — run `attic init --mono-remote %s` instead", branch, remote, remote)
			}
			if err := (gitwrap.Repo{}).Stream("clone", "--bare", "--branch", branch, "--single-branch", remote, bare); err != nil {
				return err
			}
		} else {
			if err := (gitwrap.Repo{}).Stream("clone", "--bare", remote, bare); err != nil {
				return err
			}
		}

		if err := ensureOverlayExclude(bare); err != nil {
			return err
		}
		repo := gitwrap.Repo{GitDir: bare, WorkTree: hr.Root}
		if cloneFlags.mono {
			if err := repo.Stream("config", "push.default", "current"); err != nil {
				return err
			}
			if err := repo.Stream("config", "push.autoSetupRemote", "true"); err != nil {
				return err
			}
			// `git clone --bare` sets no fetch refspec and no remote-tracking refs, so the overlay would
			// report no-upstream and couldn't compute sync state or pull. Give it a refspec covering
			// its own branch alone, then fetch and set the branch's upstream.
			if err := repo.Stream("config", "remote.origin.fetch", monoFetchRefspec(branch)); err != nil {
				return err
			}
			if err := repo.Stream("fetch", "origin"); err != nil {
				return err
			}
			if err := repo.Stream("branch", "--set-upstream-to=origin/"+branch, branch); err != nil {
				return err
			}
		}

		// Enumerate tracked paths and refuse to clobber existing work-tree files.
		out, err := repo.Run("ls-tree", "-r", "--name-only", "HEAD")
		if err != nil {
			return err
		}
		paths := splitLines(out)
		var collisions []string
		for _, p := range paths {
			if _, err := os.Stat(filepath.Join(hr.Root, p)); err == nil {
				collisions = append(collisions, p)
			}
		}
		// A colliding path the host repo tracks already has an owner, and restoring an overlay over it
		// destroys committed content. --force deliberately does not reach these: it exists to reclaim
		// stray copies of overlay files, not to overwrite host history.
		tracked, err := hostTrackedPaths(hr, collisions)
		if err != nil {
			return err
		}
		if len(tracked) > 0 {
			return fmt.Errorf("clone: %d colliding path(s) are tracked by the host repo, which owns them "+
				"— --force does not cover these:\n  %s", len(tracked), strings.Join(tracked, "\n  "))
		}
		if len(collisions) > 0 && !cloneFlags.force {
			return fmt.Errorf("clone: %d existing file(s) would be overwritten — pass --force to proceed:\n  %s",
				len(collisions), strings.Join(collisions, "\n  "))
		}

		if err := repo.Stream("checkout", "HEAD", "--", "."); err != nil {
			return err
		}

		// Re-establish the host .gitignore block for the restored paths. Without it a
		// fresh clone's files are un-ignored, and the next host `git add -A` would stage
		// the whole overlay. Eject afterwards in case any path was already tracked.
		tops := topLevels(paths)
		blk, err := ignore.Load(gitignorePath(hr))
		if err != nil {
			return err
		}
		blk.Add(tops...)
		if err := blk.Save(); err != nil {
			return err
		}
		if err := ejectFromHost(hr, tops); err != nil {
			return err
		}

		m := store.Meta{
			Fingerprint: fp,
			HostRoot:    hr.Root,
			HostName:    hr.Name(),
			OriginURL:   hr.OriginURL,
			Remote:      remote,
			Branch:      branch,
			Mono:        cloneFlags.mono,
			CreatedAt:   time.Now().UTC(),
		}
		if slug, ok := hr.OwnerRepo(); ok {
			m.Label = slug
			m.LabelSource = store.LabelSourceOrigin
		}
		if err := store.SaveMeta(m); err != nil {
			return err
		}
		provisioned = true
		modeNote := ""
		if cloneFlags.mono {
			modeNote = " (mono)"
		}
		fmt.Printf("attic: cloned overlay for %s\n  bare:   %s\n  fp:     %s\n  branch: %s\n  remote: %s%s\n  files:  %d\n",
			hr.Root, bare, fp, branch, remote, modeNote, len(paths))
		return nil
	},
}

// monoRefPatterns match refs only a shared mono remote carries: one orphan branch per overlay
// fingerprint, plus the label map.
var monoRefPatterns = []string{overlayBranchPrefix + "*", labelsBranch}

// remoteLooksMono reports whether a remote carries mono-overlay refs. One ls-remote ahead of the
// clone costs a round-trip the --mono path already pays, and saves unpicking a bare cloned in the
// wrong mode: every other project's history, and the label branch checked out over the host repo.
func remoteLooksMono(remote string) (bool, error) {
	args := append([]string{"ls-remote", "--heads", remote}, monoRefPatterns...)
	out, err := (gitwrap.Repo{}).Run(args...)
	if err != nil {
		return false, fmt.Errorf("clone: ls-remote %s: %w", remote, err)
	}
	return strings.TrimSpace(out) != "", nil
}

func init() {
	cloneCmd.Flags().BoolVar(&cloneFlags.force, "force", false, "Overwrite untracked files in the work tree (never paths the host repo tracks).")
	cloneCmd.Flags().BoolVar(&cloneFlags.mono, "mono", false, "Treat <remote> as a shared mono repo and clone only branch repo/<fp>.")
	root.AddCommand(cloneCmd)
}

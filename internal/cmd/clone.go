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

Refuses to clobber existing files unless --force.`,
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
		if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
			return fmt.Errorf("clone: mkdir parent: %w", err)
		}

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
			// report no-upstream and couldn't compute sync state or pull. Restore the standard refspec
			// `git init` + `remote add` would have given it, then fetch and set the branch's upstream.
			if err := repo.Stream("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
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
		paths := strings.Split(strings.TrimSpace(out), "\n")
		var collisions []string
		for _, p := range paths {
			if p == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(hr.Root, p)); err == nil {
				collisions = append(collisions, p)
			}
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
		modeNote := ""
		if cloneFlags.mono {
			modeNote = " (mono)"
		}
		fmt.Printf("attic: cloned overlay for %s\n  bare:   %s\n  fp:     %s\n  branch: %s\n  remote: %s%s\n  files:  %d\n",
			hr.Root, bare, fp, branch, remote, modeNote, len(paths))
		return nil
	},
}

func init() {
	cloneCmd.Flags().BoolVar(&cloneFlags.force, "force", false, "Overwrite existing files in the work tree.")
	cloneCmd.Flags().BoolVar(&cloneFlags.mono, "mono", false, "Treat <remote> as a shared mono repo and clone only branch host/<fp>.")
	root.AddCommand(cloneCmd)
}

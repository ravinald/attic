package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var cloneFlags struct {
	force bool
	mono  bool
}

var cloneCmd = &cobra.Command{
	Use:   "clone <remote>",
	Short: "Restore an existing overlay into the current host repo from its remote.",
	Long: `Clones the bare overlay locally and checks out tracked paths into the host work tree.

For a per-host-repo remote (default): clones the whole bare from <remote>.
For a shared mono remote (--mono): clones only the host/<fp> branch from <remote>, where <fp> is the host repo's fingerprint.

Refuses to clobber existing files unless --force.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		remote := args[0]
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
			branch = "host/" + fp
			// Pre-flight: confirm the branch exists on the mono remote so the user gets a useful error.
			out, err := exec.Command("git", "ls-remote", remote, branch).Output()
			if err != nil {
				return fmt.Errorf("clone: ls-remote %s: %w", remote, err)
			}
			if strings.TrimSpace(string(out)) == "" {
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

		repo := gitwrap.Repo{GitDir: bare, WorkTree: hr.Root}
		if cloneFlags.mono {
			if err := repo.Stream("config", "push.default", "current"); err != nil {
				return err
			}
			if err := repo.Stream("config", "push.autoSetupRemote", "true"); err != nil {
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

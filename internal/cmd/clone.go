package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
For a shared mono remote (--mono): clones only the repo/<fp> branch from <remote>, where <fp> is the host repo's fingerprint.

With no <remote>, clone uses this machine's default mono remote: the global mono.remote setting, else the one mono remote this machine's overlays already use. --mono is implied there. Set the default once per machine with 'attic config set --global mono.remote <url>'; the first mono init or clone sets it when unset.

Refuses to overwrite existing files that differ from the overlay's copy unless --force, and refuses paths the host repo tracks either way.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		remote, mono, err := resolveCloneRemote(args, cloneFlags.mono)
		if err != nil {
			return err
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
		if !mono {
			looksMono, err := remoteLooksMono(remote)
			if err != nil {
				return err
			}
			if looksMono {
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
		if mono {
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
		if mono {
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
		blobs, err := overlayBlobs(repo)
		if err != nil {
			return err
		}
		paths := make([]string, 0, len(blobs))
		var collisions []string
		for _, b := range blobs {
			paths = append(paths, b.path)
			if _, err := os.Lstat(filepath.Join(hr.Root, b.path)); err == nil {
				collisions = append(collisions, b.path)
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
		// A file already holding the overlay's exact bytes is not at risk: checkout rewrites it unchanged.
		// Counting it would force --force onto a copy made by hand, and --force then also covers the
		// files that do differ.
		collisions, identical, err := dropIdentical(repo, hr.Root, blobs, collisions)
		if err != nil {
			return err
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
			Mono:        mono,
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
		if mono {
			modeNote = " (mono)"
		}
		sameNote := ""
		if identical > 0 {
			sameNote = fmt.Sprintf(" (%d already identical on disk)", identical)
		}
		fmt.Printf("attic: cloned overlay for %s\n  bare:   %s\n  fp:     %s\n  branch: %s\n  remote: %s%s\n  files:  %d%s\n",
			hr.Root, bare, fp, branch, remote, modeNote, len(paths), sameNote)
		if mono {
			seedMonoRemote(remote)
		}
		return nil
	},
}

// resolveCloneRemote picks the remote and mode. A URL the user typed keeps the mode the flag gives it,
// so a typo cannot silently switch modes; with no URL the remote comes from the machine default, which
// is a mono remote by definition, so --mono is implied.
func resolveCloneRemote(args []string, monoFlag bool) (string, bool, error) {
	if len(args) == 1 {
		return args[0], monoFlag, nil
	}
	r, err := defaultMonoRemote()
	if err != nil {
		return "", false, fmt.Errorf("clone: %w", err)
	}
	return r, true, nil
}

// overlayBlob is one entry of the overlay's HEAD tree.
type overlayBlob struct {
	mode, oid, path string
}

// overlayBlobs lists every file in the overlay's HEAD tree with its object id, so a collision can be
// judged by content rather than by existence alone.
func overlayBlobs(repo gitwrap.Repo) ([]overlayBlob, error) {
	out, err := repo.Run("ls-tree", "-r", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	var blobs []overlayBlob
	for rec := range strings.SplitSeq(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta) // <mode> <type> <oid>
		if len(f) != 3 {
			return nil, fmt.Errorf("clone: unexpected ls-tree record %q", rec)
		}
		blobs = append(blobs, overlayBlob{mode: f[0], oid: f[2], path: path})
	}
	return blobs, nil
}

// dropIdentical removes from collisions every regular file whose content already hashes to the
// overlay's blob, and returns how many it dropped. Symlinks, submodules and anything that is not a
// regular file on disk stay collisions: hash-object follows a symlink, so it would compare the wrong
// bytes.
func dropIdentical(repo gitwrap.Repo, root string, blobs []overlayBlob, collisions []string) ([]string, int, error) {
	want := make(map[string]overlayBlob, len(blobs))
	for _, b := range blobs {
		want[b.path] = b
	}
	var candidates, abs []string
	var kept []string
	for _, p := range collisions {
		b := want[p]
		fi, err := os.Lstat(filepath.Join(root, p))
		if err != nil || !fi.Mode().IsRegular() || (b.mode != "100644" && b.mode != "100755") {
			kept = append(kept, p)
			continue
		}
		candidates = append(candidates, p)
		abs = append(abs, filepath.Join(root, p))
	}
	if len(candidates) == 0 {
		return kept, 0, nil
	}
	out, err := repo.Run(append([]string{"hash-object", "--"}, abs...)...)
	if err != nil {
		return nil, 0, fmt.Errorf("clone: hash existing files: %w", err)
	}
	oids := splitLines(out)
	if len(oids) != len(candidates) {
		return nil, 0, fmt.Errorf("clone: hash-object returned %d ids for %d files", len(oids), len(candidates))
	}
	identical := 0
	for i, p := range candidates {
		if oids[i] == want[p].oid {
			identical++
			continue
		}
		kept = append(kept, p)
	}
	sort.Strings(kept)
	return kept, identical, nil
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
	cloneCmd.Flags().BoolVar(&cloneFlags.mono, "mono", false, "Treat <remote> as a shared mono repo and clone only branch repo/<fp> (implied when <remote> is omitted).")
	root.AddCommand(cloneCmd)
}

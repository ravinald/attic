package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ravinald/attic/internal/gh"
	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var initFlags struct {
	remote     string
	monoRemote string
	ghPrivate  bool
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create an overlay for the current host repo.",
	Long: `Detects the git repo containing cwd, creates a bare overlay under $XDG_DATA_HOME/attic, and optionally wires a remote.

Two remote shapes:
  --remote URL        per-host-repo remote (origin/main). One GitHub repo per host.
  --mono-remote URL   shared remote with one branch per fingerprint (host/<fp>). One GitHub repo for ALL hosts.

--gh-private creates a private per-host repo via the gh CLI (mutually exclusive with the others).`,
	RunE: func(_ *cobra.Command, _ []string) error {
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
			return fmt.Errorf("overlay already exists at %s", bare)
		}
		if err := os.MkdirAll(bare, 0o755); err != nil {
			return fmt.Errorf("init: mkdir %s: %w", bare, err)
		}

		mode, err := resolveMode(initFlags.remote, initFlags.monoRemote, initFlags.ghPrivate)
		if err != nil {
			return err
		}

		branch := "main"
		if mode == modeMono {
			branch = overlayBranch(fp)
		}
		if err := (gitwrap.Repo{}).Stream("init", "--bare", "-b", branch, bare); err != nil {
			return err
		}
		if err := ensureOverlayExclude(bare); err != nil {
			return err
		}
		repo := gitwrap.Repo{GitDir: bare, WorkTree: hr.Root}

		remote := initFlags.remote
		switch mode {
		case modeMono:
			remote = initFlags.monoRemote
		case modeGhPrivate:
			ghName := hr.Name() + "-attic"
			url, err := gh.CreatePrivate(ghName, "attic overlay for "+hr.Name())
			if err != nil {
				return err
			}
			remote = url
		}

		if remote != "" {
			if err := repo.Stream("remote", "add", "origin", remote); err != nil {
				return err
			}
		}
		if mode == modeMono {
			// Make plain `attic push` route to the matching branch on origin and create it on first push.
			if err := repo.Stream("config", "push.default", "current"); err != nil {
				return err
			}
			if err := repo.Stream("config", "push.autoSetupRemote", "true"); err != nil {
				return err
			}
		}

		m := store.Meta{
			Fingerprint: fp,
			HostRoot:    hr.Root,
			HostName:    hr.Name(),
			OriginURL:   hr.OriginURL,
			Remote:      remote,
			Branch:      branch,
			Mono:        mode == modeMono,
			CreatedAt:   time.Now().UTC(),
		}
		if slug, ok := hr.OwnerRepo(); ok {
			m.Label = slug
			m.LabelSource = store.LabelSourceOrigin
		}
		if err := store.SaveMeta(m); err != nil {
			return err
		}

		fmt.Printf("attic: initialised overlay for %s\n  bare:   %s\n  fp:     %s\n  branch: %s\n", hr.Root, bare, fp, branch)
		switch {
		case remote != "" && mode == modeMono:
			fmt.Printf("  remote: %s (mono)\n", remote)
		case remote != "":
			fmt.Printf("  remote: %s\n", remote)
		default:
			fmt.Println("  remote: (none — set later with `attic exec -- remote add origin <url>`)")
		}
		if mode == modeMono {
			publishMonoLabels(remote)
		}
		return nil
	},
}

// publishMonoLabels adds this host's label to the shared map on every mono init. On the very first
// init it also seeds the _attic/labels branch and points the repo's default branch at it so the map
// is the landing page. Best-effort: a fresh overlay is already usable, so network or gh failures
// downgrade to a printed hint rather than an error that would strand init.
func publishMonoLabels(remote string) {
	out, err := exec.Command("git", "ls-remote", "--heads", remote, labelsBranch).Output()
	if err != nil {
		fmt.Printf("attic: skipped labels publish (couldn't reach %s): %v\n", remote, err)
		return
	}
	firstInit := strings.TrimSpace(string(out)) == ""

	if err := pushLabelsFor(remote); err != nil {
		fmt.Printf("attic: labels publish skipped: %v\n", err)
		return
	}
	if !firstInit {
		return // the push above landed this host; the branch is already the repo's landing page
	}

	slug, ok := host.ParseOwnerRepo(remote)
	if !ok {
		return
	}
	if !gh.Available() {
		fmt.Printf("attic: set the repo default branch to %s so the map is the landing page (gh not found)\n", labelsBranch)
		return
	}
	if err := gh.SetDefaultBranch(slug, labelsBranch); err != nil {
		fmt.Printf("attic: set the repo default branch to %s manually: %v\n", labelsBranch, err)
		return
	}
	fmt.Printf("attic: default branch set to %s — the map is now the repo landing page\n", labelsBranch)
}

type initMode int

const (
	modeNone initMode = iota
	modeRemote
	modeMono
	modeGhPrivate
)

// resolveMode enforces mutual exclusion of --remote / --mono-remote / --gh-private.
func resolveMode(remote, monoRemote string, ghPrivate bool) (initMode, error) {
	count := 0
	m := modeNone
	if remote != "" {
		count++
		m = modeRemote
	}
	if monoRemote != "" {
		count++
		m = modeMono
	}
	if ghPrivate {
		count++
		m = modeGhPrivate
	}
	if count > 1 {
		return modeNone, fmt.Errorf("init: --remote, --mono-remote, --gh-private are mutually exclusive")
	}
	return m, nil
}

func init() {
	initCmd.Flags().StringVar(&initFlags.remote, "remote", "", "Per-host-repo remote URL (origin/main).")
	initCmd.Flags().StringVar(&initFlags.monoRemote, "mono-remote", "", "Shared mono remote URL — overlay is pushed to branch host/<fp> on this repo.")
	initCmd.Flags().BoolVar(&initFlags.ghPrivate, "gh-private", false, "Create a private per-host GitHub repo via `gh` and use it as origin.")
	root.AddCommand(initCmd)
}

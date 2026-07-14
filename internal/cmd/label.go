package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

const (
	labelsBranch = "_attic/labels"
	labelsFile   = "labels.toml"
	labelsReadme = "README.md"
)

// labelsDoc is the on-wire shape of labels.toml on the mono remote.
type labelsDoc struct {
	Hosts map[string]labelEntry `toml:"hosts"`
}

type labelEntry struct {
	Label string `toml:"label"`
}

var labelCmd = &cobra.Command{
	Use:   "label",
	Short: "Get or set the display name for the current overlay.",
}

var labelGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Print the current overlay's display name (override, else map/auto, else basename).",
	RunE: func(_ *cobra.Command, _ []string) error {
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		m, err := store.LoadMeta(hr.Fingerprint())
		if err != nil {
			return err
		}
		fmt.Println(resolveLabel(m))
		return nil
	},
}

var labelSetFlags struct {
	unset bool
}

var labelSetCmd = &cobra.Command{
	Use:   "set [label]",
	Short: "Set a local display name for the current overlay (this machine only, never pushed).",
	Long: `Writes a local override to ~/.config/attic/overrides.toml — a per-machine display name that
never leaves this machine. The shared map on the mono remote stays the source of truth; change that
with 'attic labels edit'. Use --unset to drop the override and fall back to the map/auto name.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		fp := hr.Fingerprint()
		if _, err := store.LoadMeta(fp); err != nil {
			return err
		}
		if labelSetFlags.unset {
			if err := store.SetOverride(fp, ""); err != nil {
				return err
			}
			fmt.Printf("attic: cleared local label override for %s\n", fp)
			return nil
		}
		if len(args) != 1 {
			return errors.New("label set: provide a <label>, or use --unset to clear it")
		}
		label := strings.TrimSpace(args[0])
		if err := validLabel(label); err != nil {
			return err
		}
		if err := store.SetOverride(fp, label); err != nil {
			return err
		}
		fmt.Printf("attic: local label for %s set to %q (this machine only)\n", fp, label)
		return nil
	},
}

var labelResetFlags struct {
	force bool
}

var labelResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear ALL local label overrides on this machine (force-reset to the shared/auto names).",
	Long: `Wipes ~/.config/attic/overrides.toml, so every overlay falls back to its shared-map or
auto-derived name. Without --force it only lists what would be cleared.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		ov, err := store.LoadOverrides()
		if err != nil {
			return err
		}
		if len(ov) == 0 {
			fmt.Println("attic: no local overrides to clear")
			return nil
		}
		fps := make([]string, 0, len(ov))
		for fp := range ov {
			fps = append(fps, fp)
		}
		sort.Strings(fps)
		if !labelResetFlags.force {
			fmt.Printf("attic: %d local override(s) would be cleared (pass --force to apply):\n", len(ov))
			for _, fp := range fps {
				fmt.Printf("  %s  %s\n", fp, ov[fp])
			}
			return nil
		}
		if err := store.ClearOverrides(); err != nil {
			return err
		}
		fmt.Printf("attic: cleared %d local override(s) — names fall back to the shared map\n", len(ov))
		return nil
	},
}

// resolveLabel is an overlay's display name in precedence order: a local override, then the auto or
// last-pulled label in meta, then the host basename. resolveLabelWith avoids a per-row overrides read.
func resolveLabel(m store.Meta) string {
	ov, _ := store.LoadOverrides()
	return resolveLabelWith(m, ov)
}

func resolveLabelWith(m store.Meta, overrides map[string]string) string {
	if l, ok := overrides[m.Fingerprint]; ok {
		return l
	}
	return m.DisplayLabel()
}

// labelsCmd (plural) owns the multi-machine sync over the mono remote.
var labelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "Sync the host-id → label mapping across machines via the mono remote.",
}

var labelsPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Publish local labels for this mono remote to the _attic/labels branch.",
	RunE: func(_ *cobra.Command, _ []string) error {
		m, err := currentMonoMeta()
		if err != nil {
			return err
		}
		return pushLabelsFor(m.Remote)
	},
}

// pushLabelsFor contributes this machine's overlay names to the mono remote's _attic/labels map,
// regenerating labels.toml and the browsable README. It is contribute-only: a fingerprint already in
// the map is left untouched, so `attic labels edit` stays the single authority over existing names
// and no machine's push can overwrite a curated label. Shared by `labels push` and `doctor --push`.
func pushLabelsFor(remote string) error {
	local := collectLocalLabels(remote)
	if len(local) == 0 {
		return fmt.Errorf("labels push: no local overlays found for %s", remote)
	}
	dir, repo, cleanup, err := openLabelsWorktree(remote)
	if err != nil {
		return err
	}
	defer cleanup()

	merged, err := readLabelsDoc(filepath.Join(dir, labelsFile))
	if err != nil {
		return err
	}
	if merged.Hosts == nil {
		merged.Hosts = map[string]labelEntry{}
	}
	for fp, e := range local {
		if _, exists := merged.Hosts[fp]; !exists {
			merged.Hosts[fp] = e
		}
	}
	return writeCommitPushLabels(dir, repo, remote, merged)
}

// writeCommitPushLabels renders labels.toml + the README map for doc, then commits and pushes them to
// the remote's _attic/labels branch. A no-op diff is reported, not an error. Shared by push and edit.
func writeCommitPushLabels(dir string, repo gitwrap.Repo, remote string, doc labelsDoc) error {
	if err := writeLabelsDoc(filepath.Join(dir, labelsFile), doc); err != nil {
		return err
	}
	if err := writeLabelsReadme(filepath.Join(dir, labelsReadme), doc, remote); err != nil {
		return err
	}
	if err := repo.Stream("add", labelsFile, labelsReadme); err != nil {
		return err
	}
	clean, err := repo.Run("status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(clean) == "" {
		fmt.Printf("attic: labels already in sync on %s — nothing to push\n", remote)
		return nil
	}
	hostname, _ := os.Hostname()
	if err := repo.Stream("commit", "-m", "labels: push from "+hostname); err != nil {
		return err
	}
	if err := repo.Stream("push", "origin", "HEAD:"+labelsBranch); err != nil {
		return err
	}
	fmt.Printf("attic: pushed %d label(s) to %s on %s\n", len(doc.Hosts), labelsBranch, remote)
	return nil
}

var labelsPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Cache the shared map's names into local overlay labels (a local override still wins).",
	RunE: func(_ *cobra.Command, _ []string) error {
		m, err := currentMonoMeta()
		if err != nil {
			return err
		}
		dir, _, cleanup, err := openLabelsWorktree(m.Remote)
		if err != nil {
			return err
		}
		defer cleanup()
		doc, err := readLabelsDoc(filepath.Join(dir, labelsFile))
		if err != nil {
			return err
		}
		if len(doc.Hosts) == 0 {
			fmt.Println("attic: no labels published yet on", m.Remote)
			return nil
		}

		localByFP := map[string]store.Meta{}
		metas, err := store.EnumerateMetas()
		if err != nil {
			return err
		}
		for _, lm := range metas {
			localByFP[lm.Fingerprint] = lm
		}

		applied := 0
		var unknown []string
		fps := make([]string, 0, len(doc.Hosts))
		for fp := range doc.Hosts {
			fps = append(fps, fp)
		}
		sort.Strings(fps)
		for _, fp := range fps {
			entry := doc.Hosts[fp]
			lm, ok := localByFP[fp]
			if !ok {
				unknown = append(unknown, fmt.Sprintf("  %s  %s", fp, entry.Label))
				continue
			}
			if lm.Label == entry.Label {
				continue
			}
			lm.Label = entry.Label
			if err := store.SaveMeta(lm); err != nil {
				return err
			}
			applied++
		}
		fmt.Printf("attic: applied %d label update(s)\n", applied)
		if len(unknown) > 0 {
			fmt.Println("attic: labels for overlays not initialised on this machine:")
			fmt.Println(strings.Join(unknown, "\n"))
		}
		return nil
	},
}

// currentMonoMeta loads the current overlay's metadata and refuses non-mono setups.
func currentMonoMeta() (store.Meta, error) {
	hr, err := resolveHost()
	if err != nil {
		return store.Meta{}, err
	}
	m, err := store.LoadMeta(hr.Fingerprint())
	if err != nil {
		return store.Meta{}, err
	}
	if !m.Mono || m.Remote == "" {
		return store.Meta{}, errors.New("labels: current overlay is not on a mono remote — labels sync only applies to mono mode")
	}
	return m, nil
}

// collectLocalLabels returns every local overlay sharing the given mono remote, keyed by fingerprint.
// Overlays with no explicit Label fall back to HostName so first-time pushes carry useful names.
func collectLocalLabels(remote string) map[string]labelEntry {
	out := map[string]labelEntry{}
	metas, err := store.EnumerateMetas()
	if err != nil {
		return out
	}
	for _, m := range metas {
		if !m.Mono || m.Remote != remote {
			continue
		}
		out[m.Fingerprint] = labelEntry{Label: m.DisplayLabel()}
	}
	return out
}

// openLabelsWorktree clones the mono remote into a temp dir checked out on the _attic/labels branch
// (creating it as an orphan if absent). The returned cleanup removes the temp dir.
func openLabelsWorktree(remote string) (string, gitwrap.Repo, func(), error) {
	dir, err := os.MkdirTemp("", "attic-labels-*")
	if err != nil {
		return "", gitwrap.Repo{}, func() {}, fmt.Errorf("labels: tempdir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	repo := gitwrap.Repo{GitDir: filepath.Join(dir, ".git"), WorkTree: dir}
	if err := (gitwrap.Repo{}).Stream("init", "-q", "-b", labelsBranch, dir); err != nil {
		cleanup()
		return "", gitwrap.Repo{}, func() {}, err
	}
	if err := repo.Stream("remote", "add", "origin", remote); err != nil {
		cleanup()
		return "", gitwrap.Repo{}, func() {}, err
	}
	// Try to fetch the labels branch; absence is fine — we'll create it as orphan locally.
	out, _ := repo.Run("ls-remote", "--heads", "origin", labelsBranch)
	if strings.TrimSpace(out) != "" {
		if err := repo.Stream("fetch", "--depth=1", "origin", labelsBranch); err != nil {
			cleanup()
			return "", gitwrap.Repo{}, func() {}, err
		}
		if err := repo.Stream("reset", "--hard", "FETCH_HEAD"); err != nil {
			cleanup()
			return "", gitwrap.Repo{}, func() {}, err
		}
	}
	return dir, repo, cleanup, nil
}

func readLabelsDoc(path string) (labelsDoc, error) {
	var d labelsDoc
	_, err := toml.DecodeFile(path, &d)
	if err != nil {
		if os.IsNotExist(err) {
			return labelsDoc{Hosts: map[string]labelEntry{}}, nil
		}
		return d, fmt.Errorf("labels: decode %s: %w", path, err)
	}
	if d.Hosts == nil {
		d.Hosts = map[string]labelEntry{}
	}
	return d, nil
}

func writeLabelsDoc(path string, d labelsDoc) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("labels: create %s: %w", tmp, err)
	}
	header := fmt.Sprintf("# attic labels — managed by `attic labels push|pull`.\n# Edits made here are preserved as long as no machine pushes a conflicting label for the same fingerprint.\n# Last touched: %s UTC\n\n", time.Now().UTC().Format(time.RFC3339))
	if _, err := f.WriteString(header); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := toml.NewEncoder(f).Encode(d); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("labels: encode: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeLabelsReadme renders the fingerprint→label map as a markdown table so the mono remote is
// browsable on the web: each row links to the overlay's host/<fp> branch. The link base is derived
// from the mono remote URL; if it can't be parsed, the branch is shown as plain text. Written to the
// _attic/labels branch only — never a host/<fp> branch, whose checkout lands in the host work tree.
func writeLabelsReadme(path string, d labelsDoc, remote string) error {
	webBase, hasWeb := host.WebBase(remote)
	fps := make([]string, 0, len(d.Hosts))
	for fp := range d.Hosts {
		fps = append(fps, fp)
	}
	sort.Strings(fps)

	var b strings.Builder
	b.WriteString("# attic overlays\n\n")
	b.WriteString("Fingerprint → label map for the overlays stored on this mono remote. ")
	b.WriteString("Managed by `attic labels push` — edit via the CLI, not by hand.\n\n")
	b.WriteString("| Label | Branch | Fingerprint |\n|---|---|---|\n")
	for _, fp := range fps {
		branch := "host/" + fp
		cell := branch
		if hasWeb {
			cell = fmt.Sprintf("[%s](%s/tree/%s)", branch, webBase, branch)
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` |\n", d.Hosts[fp].Label, cell, fp)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("labels: write %s: %w", labelsReadme, err)
	}
	return os.Rename(tmp, path)
}

// validLabel rejects empty strings, whitespace, and backslashes. An interior "/" is allowed so
// labels can carry an "owner/repo" slug, but leading/trailing slashes, "//", and ".." segments are
// refused as path-traversal hygiene — labels are display-only today, but nothing should assume so.
func validLabel(s string) error {
	if s == "" {
		return errors.New("label: must not be empty (use a non-empty name)")
	}
	if strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
		return errors.New("label: must not start or end with '/'")
	}
	if strings.Contains(s, "//") {
		return errors.New("label: must not contain '//'")
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return errors.New("label: must not contain whitespace")
		}
		if r == '\\' {
			return errors.New("label: must not contain a backslash")
		}
	}
	if slices.Contains(strings.Split(s, "/"), "..") {
		return errors.New("label: must not contain a '..' path segment")
	}
	return nil
}

func init() {
	labelSetCmd.Flags().BoolVar(&labelSetFlags.unset, "unset", false, "Clear the local override and fall back to the map/auto name.")
	labelResetCmd.Flags().BoolVar(&labelResetFlags.force, "force", false, "Actually clear the overrides (without it, only lists them).")
	labelCmd.AddCommand(labelGetCmd, labelSetCmd, labelResetCmd)
	labelsCmd.AddCommand(labelsPushCmd, labelsPullCmd)
	root.AddCommand(labelCmd)
	root.AddCommand(labelsCmd)
}

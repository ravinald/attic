package cmd

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

const (
	labelsBranch = "_attic/labels"
	labelsFile   = "labels.toml"
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
	Short: "Get or set the human-readable label for the current overlay.",
}

var labelGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Print the current overlay's label (falls back to host_name).",
	RunE: func(_ *cobra.Command, _ []string) error {
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		m, err := store.LoadMeta(hr.Fingerprint())
		if err != nil {
			return err
		}
		fmt.Println(m.DisplayLabel())
		return nil
	},
}

var labelSetCmd = &cobra.Command{
	Use:   "set <label>",
	Short: "Set the current overlay's label.",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		label := strings.TrimSpace(args[0])
		if err := validLabel(label); err != nil {
			return err
		}
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		m, err := store.LoadMeta(hr.Fingerprint())
		if err != nil {
			return err
		}
		m.Label = label
		if err := store.SaveMeta(m); err != nil {
			return err
		}
		fmt.Printf("attic: label for %s set to %q\n", m.Fingerprint, label)
		return nil
	},
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
		local := collectLocalLabels(m.Remote)
		if len(local) == 0 {
			return fmt.Errorf("labels push: no local overlays found for %s", m.Remote)
		}
		dir, repo, cleanup, err := openLabelsWorktree(m.Remote)
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
		maps.Copy(merged.Hosts, local)
		if err := writeLabelsDoc(filepath.Join(dir, labelsFile), merged); err != nil {
			return err
		}
		if err := repo.Stream("add", labelsFile); err != nil {
			return err
		}

		clean, err := repo.Run("status", "--porcelain")
		if err != nil {
			return err
		}
		if strings.TrimSpace(clean) == "" {
			fmt.Println("attic: labels already in sync — nothing to push")
			return nil
		}
		hostname, _ := os.Hostname()
		msg := fmt.Sprintf("labels: push from %s", hostname)
		if err := repo.Stream("commit", "-m", msg); err != nil {
			return err
		}
		if err := repo.Stream("push", "origin", "HEAD:"+labelsBranch); err != nil {
			return err
		}
		fmt.Printf("attic: pushed %d label(s) to %s on %s\n", len(local), labelsBranch, m.Remote)
		return nil
	},
}

var labelsPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Fetch labels from the _attic/labels branch and update local overlays.",
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

// validLabel rejects empty strings, internal whitespace, and path separators.
func validLabel(s string) error {
	if s == "" {
		return errors.New("label: must not be empty (use a non-empty name)")
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return errors.New("label: must not contain whitespace")
		}
		if r == '/' || r == '\\' {
			return errors.New("label: must not contain path separators")
		}
	}
	return nil
}

func init() {
	labelCmd.AddCommand(labelGetCmd, labelSetCmd)
	labelsCmd.AddCommand(labelsPushCmd, labelsPullCmd)
	root.AddCommand(labelCmd)
	root.AddCommand(labelsCmd)
}

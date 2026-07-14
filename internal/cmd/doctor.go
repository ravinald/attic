package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var doctorFlags struct {
	fix   bool
	force bool
	push  bool
}

// finding is one thing doctor noticed about an overlay. A finding is either fixable (an auto-derived
// label drifted, is unset, or the origin url moved), protected (a hand-set label that no longer
// matches origin — reported, changed only with --force), or an anomaly (missing storage/host —
// reported, never auto-changed).
type finding struct {
	fp        string
	label     string
	kind      string
	detail    string
	newLabel  string // non-empty → apply as the overlay's label
	newOrigin string // non-empty → refresh the overlay's stored origin url
	fixable   bool
	protected bool
	anomaly   bool
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Audit every overlay's label against its origin remote and report (or fix) drift.",
	Long: `Sweeps every overlay on this machine and compares its label to the "owner/repo" slug of its
host repo's current origin remote.

Without flags it only reports, exiting non-zero when fixable drift exists (suitable for a hook).
--fix rewrites auto-derived labels and refreshes moved origin URLs in the local meta.
--force additionally adopts the origin slug over a hand-set label.
--fix --push publishes the corrected map to each affected mono remote (chains 'attic labels push').

Plain --fix stays local — offline and CI runs never touch the network unless you ask with --push.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if doctorFlags.push && !doctorFlags.fix {
			return fmt.Errorf("doctor: --push requires --fix (nothing to publish without fixing first)")
		}
		metas, err := store.EnumerateMetas()
		if err != nil {
			return err
		}
		if len(metas) == 0 {
			fmt.Println("attic: no overlays on this machine")
			return nil
		}

		var findings []finding
		for _, m := range metas {
			if f := classify(m); f != nil {
				findings = append(findings, *f)
			}
		}
		sort.Slice(findings, func(i, j int) bool { return findings[i].fp < findings[j].fp })

		if len(findings) == 0 {
			fmt.Printf("attic: %d overlay(s) checked — labels in sync\n", len(metas))
			return nil
		}

		if !doctorFlags.fix {
			printFindings(cmd.OutOrStdout(), findings)
			fixable := 0
			for _, f := range findings {
				if f.fixable {
					fixable++
				}
			}
			if fixable > 0 {
				return fmt.Errorf("%d overlay(s) with fixable drift — run `attic doctor --fix`", fixable)
			}
			return nil
		}

		applied, skipped, remotes := applyFindings(findings)
		fmt.Printf("attic: fixed %d overlay(s)\n", applied)
		if len(skipped) > 0 {
			fmt.Println("attic: left untouched (advisory — rerun with --force to adopt manual labels, or resolve by hand):")
			printFindings(os.Stdout, skipped)
		}
		if !doctorFlags.push {
			if applied > 0 {
				fmt.Println("attic: run `attic labels push` to publish the corrected map (or pass --push next time)")
			}
			return nil
		}
		for _, remote := range remotes {
			if err := pushLabelsFor(remote); err != nil {
				return err
			}
		}
		if len(remotes) == 0 {
			fmt.Println("attic: --push: no mono-remote overlays were changed — nothing to publish")
		}
		return nil
	},
}

// classify inspects one overlay and returns its finding, or nil when nothing is wrong.
func classify(m store.Meta) *finding {
	fp := m.Fingerprint
	if bare, err := store.BareDir(fp); err == nil {
		if _, err := os.Stat(bare); err != nil {
			return &finding{fp: fp, label: m.DisplayLabel(), kind: "bare-missing", anomaly: true,
				detail: "overlay storage missing at " + bare}
		}
	}
	if _, err := os.Stat(m.HostRoot); err != nil {
		return &finding{fp: fp, label: m.DisplayLabel(), kind: "host-missing", anomaly: true,
			detail: "host repo not found at " + m.HostRoot}
	}

	effOrigin := m.OriginURL
	var newOrigin string
	if live, ok := liveOrigin(m.HostRoot); ok && live != m.OriginURL {
		effOrigin, newOrigin = live, live
	} else if ok {
		effOrigin = live
	}

	slug, hasSlug := host.ParseOwnerRepo(effOrigin)
	if !hasSlug {
		if m.Label == "" {
			return &finding{fp: fp, label: m.DisplayLabel(), kind: "no-origin", anomaly: true,
				detail: "no parseable origin remote — can't derive a label"}
		}
		return nil // has a label but no origin to check it against — leave it alone
	}

	if m.Label == slug {
		if newOrigin != "" {
			return &finding{fp: fp, label: m.Label, kind: "origin", newOrigin: newOrigin, fixable: true,
				detail: "origin url moved to " + newOrigin}
		}
		return nil
	}

	// The label disagrees with the origin slug. A hand-set label (or a pre-provenance label we can't
	// prove was auto-derived) is protected: reported, but only overwritten with --force.
	protected := m.LabelSource == store.LabelSourceManual || (m.LabelSource == "" && m.Label != "")
	if protected {
		return &finding{fp: fp, label: m.DisplayLabel(), kind: "manual", newLabel: slug, newOrigin: newOrigin,
			protected: true, detail: fmt.Sprintf("manual label %q; origin suggests %q", m.DisplayLabel(), slug)}
	}
	kind := "drift"
	if m.Label == "" {
		kind = "unset"
	}
	return &finding{fp: fp, label: m.DisplayLabel(), kind: kind, newLabel: slug, newOrigin: newOrigin,
		fixable: true, detail: fmt.Sprintf("label %q → %q", m.DisplayLabel(), slug)}
}

// applyFindings writes the fixes doctor is allowed to make. It returns how many it applied, the
// findings it deliberately left alone (anomalies, and protected labels unless --force), and the
// distinct mono remotes whose overlays it touched — the set --push must republish.
func applyFindings(findings []finding) (applied int, skipped []finding, remotes []string) {
	seen := map[string]struct{}{}
	for _, f := range findings {
		if f.anomaly || (f.protected && !doctorFlags.force) {
			skipped = append(skipped, f)
			continue
		}
		m, err := store.LoadMeta(f.fp)
		if err != nil {
			skipped = append(skipped, f)
			continue
		}
		if f.newLabel != "" {
			m.Label = f.newLabel
			m.LabelSource = store.LabelSourceOrigin
		}
		if f.newOrigin != "" {
			m.OriginURL = f.newOrigin
		}
		if err := store.SaveMeta(m); err != nil {
			skipped = append(skipped, f)
			continue
		}
		applied++
		if m.Mono && m.Remote != "" {
			if _, ok := seen[m.Remote]; !ok {
				seen[m.Remote] = struct{}{}
				remotes = append(remotes, m.Remote)
			}
		}
	}
	sort.Strings(remotes)
	return applied, skipped, remotes
}

// liveOrigin reads the host repo's current origin URL, quietly — a missing origin is information,
// not an error to escalate.
func liveOrigin(hostRoot string) (string, bool) {
	c := exec.Command("git", "-C", hostRoot, "remote", "get-url", "origin")
	c.Stderr = io.Discard
	out, err := c.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func printFindings(w io.Writer, findings []finding) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "STATUS\tFP\tLABEL\tDETAIL")
	for _, f := range findings {
		label := f.label
		if label == "" {
			label = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.kind, f.fp, label, f.detail)
	}
	_ = tw.Flush()
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFlags.fix, "fix", false, "Rewrite drifted auto-labels and refresh moved origin URLs in local meta.")
	doctorCmd.Flags().BoolVar(&doctorFlags.force, "force", false, "With --fix, also overwrite hand-set labels that drifted from origin.")
	doctorCmd.Flags().BoolVar(&doctorFlags.push, "push", false, "With --fix, publish the corrected map to each affected mono remote.")
	root.AddCommand(doctorCmd)
}

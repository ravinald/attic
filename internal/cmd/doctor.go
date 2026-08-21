package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

// findingWedged marks an overlay stopped part-way through a rebase, merge, or cherry-pick.
const findingWedged = "wedged"

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

		overrides, _ := store.LoadOverrides()
		var findings []finding
		for _, m := range metas {
			if f := classify(m, overrides); f != nil {
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
			fixable, wedged := 0, 0
			for _, f := range findings {
				switch {
				case f.kind == findingWedged:
					wedged++
				case f.fixable:
					fixable++
				}
			}
			// A wedged overlay outranks drift in the exit code: it has stopped syncing, so a hook that
			// treats doctor's zero exit as "nothing to do" would keep reporting healthy while overlay
			// history piles up locally. Other anomalies stay exit-0, as they always have.
			if wedged > 0 {
				return fmt.Errorf("%d overlay(s) stopped mid-operation and no longer syncing", wedged)
			}
			if fixable > 0 {
				return fmt.Errorf("%d overlay(s) with fixable drift — run `attic doctor --fix`", fixable)
			}
			return nil
		}

		applied, skipped, remotes := applyFindings(findings)
		fmt.Printf("attic: fixed %d overlay(s)\n", applied)
		if len(skipped) > 0 {
			fmt.Println("attic: left untouched (advisory — overridden locally, a manual label needing --force, or an anomaly to resolve by hand):")
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

// classify inspects one overlay and returns its finding, or nil when nothing is wrong. An overlay
// with a local override is reported as such and left untouched — doctor honours the local choice.
func classify(m store.Meta, overrides map[string]string) *finding {
	fp := m.Fingerprint
	if bare, err := store.BareDir(fp); err == nil {
		if _, err := os.Stat(bare); err != nil {
			return &finding{fp: fp, label: m.DisplayLabel(), kind: "bare-missing", anomaly: true,
				detail: "overlay storage missing at " + bare}
		}
	}
	if ov, ok := overrides[fp]; ok {
		return &finding{fp: fp, label: ov, kind: "overridden", anomaly: true,
			detail: fmt.Sprintf("local override %q — left alone (clear with `attic label reset`, or manage the map via `attic labels edit`)", ov)}
	}
	if _, err := os.Stat(m.HostRoot); err != nil {
		return &finding{fp: fp, label: m.DisplayLabel(), kind: "host-missing", anomaly: true,
			detail: "host repo not found at " + m.HostRoot}
	}
	// Reported, never auto-fixed: re-keying moves a storage directory, and doctor sweeps every overlay
	// on the machine. A bulk mutation of that shape wants the operator in the host repo deciding.
	if live, ok := liveFingerprint(m.HostRoot); ok && live != fp {
		return &finding{fp: fp, label: m.DisplayLabel(), kind: "fingerprint", anomaly: true,
			detail: fmt.Sprintf("host root commit now fingerprints %s (history rewritten) — run `attic rekey` in %s", live, m.HostRoot)}
	}

	// Reported ahead of any label question: a wedged overlay stops syncing entirely, and doctor is
	// the only sweep that sees every overlay on the machine. Never auto-fixed — choosing between
	// --continue and --abort is a call about which side of a conflict survives.
	if seq, err := overlaySequencer(m); err == nil && seq.InProgress() {
		return &finding{fp: fp, label: m.DisplayLabel(), kind: findingWedged, anomaly: true,
			detail: fmt.Sprintf("stopped mid-%s — resolve in %s with `attic exec %s`", seq.Op, m.HostRoot, seq.Abort)}
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

// overlaySequencer opens an overlay by fingerprint to ask whether it is stopped mid-operation.
// doctor sweeps by metadata rather than from inside a host repo, so it cannot use openOverlay.
func overlaySequencer(m store.Meta) (gitwrap.Sequencer, error) {
	bare, err := store.BareDir(m.Fingerprint)
	if err != nil {
		return gitwrap.Sequencer{}, err
	}
	return gitwrap.Repo{GitDir: bare, WorkTree: m.HostRoot}.Sequencer()
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

// liveFingerprint returns the fingerprint the host repo hashes to today. Not-ok covers a repo that
// can't be read at all, which the host-missing check already reports; only a readable repo that
// disagrees with its stored key is a re-key candidate.
func liveFingerprint(hostRoot string) (string, bool) {
	r, err := host.Detect(hostRoot)
	if err != nil {
		return "", false
	}
	return r.Fingerprint(), true
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

package cmd

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var labelsEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit the whole label map in $EDITOR and publish on save (visudo-style).",
	Long: `Opens the mono remote's fingerprint→label map in $EDITOR. On save it validates every label,
regenerates the browsable README, and pushes — the one-shot way to rename any overlay, including
"foreign" ones whose host repo lives on another machine.

Local overlays not yet in the map are surfaced so you can name them here too. Editing an entry marks
it as a manual label locally, so a later 'attic labels push' won't revert it.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		m, err := currentMonoMeta()
		if err != nil {
			return err
		}
		dir, repo, cleanup, err := openLabelsWorktree(m.Remote)
		if err != nil {
			return err
		}
		defer cleanup()

		doc, err := readLabelsDoc(filepath.Join(dir, labelsFile))
		if err != nil {
			return err
		}
		if doc.Hosts == nil {
			doc.Hosts = map[string]labelEntry{}
		}
		// Surface local overlays missing from the published map so they can be named in the same pass.
		for fp, e := range collectLocalLabels(m.Remote) {
			if _, ok := doc.Hosts[fp]; !ok {
				doc.Hosts[fp] = e
			}
		}

		edited, err := editLabels(doc)
		if err != nil {
			return err
		}
		if maps.Equal(doc.Hosts, edited.Hosts) {
			fmt.Println("attic: no label changes")
			return nil
		}
		if err := writeCommitPushLabels(dir, repo, m.Remote, edited); err != nil {
			return err
		}
		applyLabelsToLocal(edited)
		return nil
	},
}

// editLabels renders doc into a temp file, opens it in the user's editor, and parses the result.
func editLabels(doc labelsDoc) (labelsDoc, error) {
	tmp, err := os.CreateTemp("", "attic-labels-*.txt")
	if err != nil {
		return labelsDoc{}, fmt.Errorf("labels edit: tempfile: %w", err)
	}
	path := tmp.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := tmp.WriteString(renderLabels(doc)); err != nil {
		_ = tmp.Close()
		return labelsDoc{}, err
	}
	if err := tmp.Close(); err != nil {
		return labelsDoc{}, err
	}
	if err := runEditor(path); err != nil {
		return labelsDoc{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return labelsDoc{}, fmt.Errorf("labels edit: read back: %w", err)
	}
	return parseLabels(string(data))
}

// renderLabels writes one "<fingerprint>  <label>" line per entry, sorted, under a comment header.
func renderLabels(doc labelsDoc) string {
	fps := make([]string, 0, len(doc.Hosts))
	for fp := range doc.Hosts {
		fps = append(fps, fp)
	}
	sort.Strings(fps)
	var b strings.Builder
	b.WriteString("# attic labels — edit the label column, then save & quit to publish.\n")
	b.WriteString("# One entry per line:  <fingerprint>  <label>\n")
	b.WriteString("# Delete a line to drop that overlay from the map. Labels can't contain spaces.\n#\n")
	for _, fp := range fps {
		fmt.Fprintf(&b, "%s  %s\n", fp, doc.Hosts[fp].Label)
	}
	return b.String()
}

// parseLabels reads the edited two-column form back into a labelsDoc, validating each label.
func parseLabels(s string) (labelsDoc, error) {
	out := labelsDoc{Hosts: map[string]labelEntry{}}
	sc := bufio.NewScanner(strings.NewReader(s))
	for line := 1; sc.Scan(); line++ {
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) != 2 {
			return labelsDoc{}, fmt.Errorf("labels edit: line %d: expected '<fingerprint> <label>', got %q", line, t)
		}
		fp, label := fields[0], fields[1]
		if err := validLabel(label); err != nil {
			return labelsDoc{}, fmt.Errorf("labels edit: line %d: %w", line, err)
		}
		if _, dup := out.Hosts[fp]; dup {
			return labelsDoc{}, fmt.Errorf("labels edit: line %d: duplicate fingerprint %s", line, fp)
		}
		out.Hosts[fp] = labelEntry{Label: label}
	}
	if err := sc.Err(); err != nil {
		return labelsDoc{}, fmt.Errorf("labels edit: scan: %w", err)
	}
	return out, nil
}

// runEditor opens path in $VISUAL, then $EDITOR, then vi, wired to the terminal.
func runEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor) // honour editors carrying flags, e.g. "code -w"
	c := exec.Command(parts[0], append(parts[1:], path)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("labels edit: editor %q: %w", editor, err)
	}
	return nil
}

// applyLabelsToLocal mirrors edited names into the meta.toml of overlays present on this machine, as
// manual labels, so `attic list` shows them and a later push doesn't revert the edit.
func applyLabelsToLocal(doc labelsDoc) {
	metas, err := store.EnumerateMetas()
	if err != nil {
		return
	}
	for _, m := range metas {
		e, ok := doc.Hosts[m.Fingerprint]
		if !ok || m.Label == e.Label {
			continue
		}
		m.Label = e.Label
		m.LabelSource = store.LabelSourceManual
		_ = store.SaveMeta(m)
	}
}

func init() {
	labelsCmd.AddCommand(labelsEditCmd)
}

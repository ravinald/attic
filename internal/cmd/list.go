package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var listFlags struct {
	fetch  bool
	wide   bool
	asJSON bool
}

// overlayRow is a flattened view of one overlay for listing.
type overlayRow struct {
	Label     string `json:"label"`
	FP        string `json:"fp"`
	HostRoot  string `json:"host_root"`
	HostName  string `json:"host_name"`
	Remote    string `json:"remote,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Mono      bool   `json:"mono"`
	SyncState string `json:"sync"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List every overlay attic knows about on this machine.",
	Long:  "Scans $XDG_DATA_HOME/attic/repos and prints one row per overlay. Sync state uses already-fetched refs unless --fetch is passed.",
	RunE: func(_ *cobra.Command, _ []string) error {
		metas, err := store.EnumerateMetas()
		if err != nil {
			return err
		}
		overrides, _ := store.LoadOverrides()
		rows := make([]overlayRow, 0, len(metas))
		for _, m := range metas {
			rows = append(rows, buildRow(m, listFlags.fetch, overrides))
		}
		if listFlags.asJSON {
			return json.NewEncoder(os.Stdout).Encode(rows)
		}
		return formatRows(os.Stdout, rows, listFlags.wide)
	},
}

func buildRow(m store.Meta, fetch bool, overrides map[string]string) overlayRow {
	r := overlayRow{
		Label:    resolveLabelWith(m, overrides),
		FP:       m.Fingerprint,
		HostRoot: m.HostRoot,
		HostName: m.HostName,
		Remote:   m.Remote,
		Branch:   m.Branch,
		Mono:     m.Mono,
	}
	bare, err := store.BareDir(m.Fingerprint)
	if err != nil {
		r.SyncState = "unreachable"
		return r
	}
	if _, err := os.Stat(bare); err != nil {
		r.SyncState = "missing"
		return r
	}
	repo := gitwrap.Repo{GitDir: bare, WorkTree: m.HostRoot}
	r.SyncState = syncStateFor(repo, fetch)
	return r
}

// syncStateFor reports the overlay's relationship to its upstream. It never returns an error —
// listing must keep working under broken networks, missing remotes, or detached HEADs.
// All probes run with stderr discarded: a missing upstream is a normal state for a freshly
// initialised overlay and shouldn't leak `fatal:` lines into the listing output.
func syncStateFor(repo gitwrap.Repo, fetch bool) string {
	if _, err := gitQuiet(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		return "no-upstream"
	}
	if fetch {
		if _, err := gitQuiet(repo, "fetch", "--quiet"); err != nil {
			return "unreachable"
		}
	}
	out, err := gitQuiet(repo, "rev-list", "--left-right", "--count", "@{u}...HEAD")
	if err != nil {
		return "unreachable"
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return "unknown"
	}
	behind, _ := strconv.Atoi(fields[0])
	ahead, _ := strconv.Atoi(fields[1])
	if ahead == 0 && behind == 0 {
		return "clean"
	}
	return fmt.Sprintf("↑%d ↓%d", ahead, behind)
}

// gitQuiet runs git in the overlay context and discards stderr. Used for probes whose
// failure is information, not a problem to escalate.
func gitQuiet(repo gitwrap.Repo, args ...string) (string, error) {
	prefix := []string{}
	if repo.GitDir != "" {
		prefix = append(prefix, "--git-dir="+repo.GitDir)
	}
	if repo.WorkTree != "" {
		prefix = append(prefix, "--work-tree="+repo.WorkTree)
	}
	c := exec.Command("git", append(prefix, args...)...)
	c.Stderr = io.Discard
	out, err := c.Output()
	return string(out), err
}

func formatRows(w io.Writer, rows []overlayRow, wide bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if wide {
		if _, err := fmt.Fprintln(tw, "LABEL\tFP\tHOST ROOT\tBRANCH\tREMOTE\tSYNC"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(tw, "LABEL\tFP\tHOST ROOT\tBRANCH\tSYNC"); err != nil {
			return err
		}
	}
	for _, r := range rows {
		branch := r.Branch
		if branch == "" {
			branch = "-"
		}
		remote := r.Remote
		if remote == "" {
			remote = "(none)"
		}
		sync := r.SyncState
		if sync == "" {
			sync = "-"
		}
		if wide {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Label, r.FP, r.HostRoot, branch, remote, sync); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				r.Label, r.FP, r.HostRoot, branch, sync); err != nil {
				return err
			}
		}
	}
	return tw.Flush()
}

func init() {
	listCmd.Flags().BoolVar(&listFlags.fetch, "fetch", false, "Fetch each overlay's remote before computing sync state (slower, network).")
	listCmd.Flags().BoolVar(&listFlags.wide, "wide", false, "Include the remote URL column.")
	listCmd.Flags().BoolVar(&listFlags.asJSON, "json", false, "Emit JSON instead of a table.")
	root.AddCommand(listCmd)
}

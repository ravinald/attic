package cmd

import (
	"fmt"
	"strings"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:                "status",
	Short:              "Show overlay status (git status), plus overlay files git can't see.",
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		hr, repo, err := openOverlay()
		if err != nil {
			return err
		}
		if err := repo.Stream(append([]string{"status"}, args...)...); err != nil {
			return err
		}
		f := parseStatusFormat(args)
		if f.machine && f.ignored {
			// git's own stream already carries these files, as `!!`/`!`, so a second copy would
			// double-report them to whoever is parsing. A human still gets the section below: it is
			// what separates the overlay's files from the host tree git just listed alongside them.
			return nil
		}
		scope, err := overlayScope(hr, repo)
		if err != nil {
			return err
		}
		untracked, err := reportableUntracked(repo, scope)
		if err != nil {
			return err
		}
		if len(untracked) == 0 {
			return nil
		}
		if f.machine {
			return printUntrackedPorcelain(repo, f, untracked)
		}
		// git printed its verdict above without these files in view — the host .gitignore hides them
		// from it — so the header has to explain why "working tree clean" and this list coexist.
		fmt.Println()
		fmt.Println("Untracked overlay files (hidden from git by the host .gitignore):")
		fmt.Println("  (use \"attic add <file>...\" to include in what will be committed)")
		fmt.Println()
		for _, p := range untracked {
			fmt.Printf("\t%s\n", p)
		}
		fmt.Println()
		return nil
	},
}

// statusFormat describes the stream the caller asked git for. The prose section is attic's own, but
// the file list underneath it is data git has a defined encoding for, so a machine-readable run
// re-renders the list in that encoding rather than dropping it. Suppressing it entirely told every
// script gating on `attic status --porcelain` that a brand-new overlay file did not exist.
type statusFormat struct {
	machine bool // output a program parses, not a human
	v2      bool // --porcelain=v2 marks untracked with a single "?"
	nul     bool // -z: NUL-terminated records, and git emits paths raw
	ignored bool // --ignored: git already listed the overlay's files itself
}

func parseStatusFormat(args []string) statusFormat {
	var f statusFormat
	for _, a := range args {
		switch {
		case a == "-s", a == "--short", a == "--porcelain":
			f.machine = true
		case a == "-z":
			f.machine, f.nul = true, true
		case strings.HasPrefix(a, "--porcelain="):
			f.machine = true
			f.v2 = strings.TrimPrefix(a, "--porcelain=") == "v2"
		case a == "--ignored", strings.HasPrefix(a, "--ignored="):
			f.ignored = true
		case a == "--": // everything after is a pathspec
			return f
		case isShortFlagCluster(a):
			f.machine = f.machine || strings.ContainsAny(a, "sz")
			f.nul = f.nul || strings.Contains(a, "z")
		}
	}
	return f
}

// isShortFlagCluster reports whether a is git's bundled short-flag form, as in `status -sz`. Only
// clusters built entirely from status's valueless short flags qualify: `-uall` is `-u` carrying a
// value, and reading its letters as flags would be a guess at what the caller meant.
func isShortFlagCluster(a string) bool {
	if len(a) < 3 || !strings.HasPrefix(a, "-") || strings.HasPrefix(a, "--") {
		return false
	}
	return strings.Trim(a[1:], "szbvu") == ""
}

// printUntrackedPorcelain writes the overlay's untracked files in the caller's own format, using the
// untracked marker rather than the ignored one: git ignores them only because the host .gitignore
// hides what the overlay owns, and to attic they are files someone still has to stage.
func printUntrackedPorcelain(repo gitwrap.Repo, f statusFormat, files []string) error {
	marker := "??"
	if f.v2 {
		marker = "?"
	}
	if f.nul {
		for _, p := range files {
			fmt.Printf("%s %s\x00", marker, p)
		}
		return nil
	}
	quoted, err := quotePaths(repo, files)
	if err != nil {
		return err
	}
	for _, p := range quoted {
		fmt.Printf("%s %s\n", marker, p)
	}
	return nil
}

// quotePaths re-asks git for the same paths without -z, so the result carries git's own C-style
// quoting. Hand-rolling that would drift from core.quotePath, and an unquoted path holding a newline
// silently corrupts a line-oriented parse. The :(literal) magic stops a name like "a[1].md" being
// read back as a glob that matches nothing.
func quotePaths(repo gitwrap.Repo, files []string) ([]string, error) {
	args := []string{"ls-files", "--others", "--ignored", "--exclude-standard", "--"}
	for _, f := range files {
		args = append(args, ":(literal)"+f)
	}
	out, err := repo.Run(args...)
	if err != nil {
		return nil, err
	}
	// Split on newlines alone: git leaves a trailing space in an unquoted path, and trimming would
	// hand back a name that does not exist on disk.
	var quoted []string
	for l := range strings.SplitSeq(out, "\n") {
		if l != "" {
			quoted = append(quoted, l)
		}
	}
	return quoted, nil
}

func init() {
	root.AddCommand(statusCmd)
}

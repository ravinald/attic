package cmd

import (
	"errors"
	"strings"
	"time"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/spf13/cobra"
)

var commitFlags struct {
	message    string
	all        bool
	allowEmpty bool
}

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit staged overlay changes.",
	RunE: func(_ *cobra.Command, args []string) error {
		hr, repo, err := openOverlay()
		if err != nil {
			return err
		}
		if err := preflight(hr, repo, args); err != nil {
			return err
		}
		// An overlay is scratch history — a message rarely earns its keep. Without -m, git would
		// drop into an editor that aborts on an empty message; synthesise a timestamped one instead.
		msg := commitFlags.message
		if msg == "" {
			msg = "attic snapshot " + time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
		}
		gitArgs := []string{"commit", "-m", msg}
		if commitFlags.all {
			gitArgs = append(gitArgs, "-a")
		}
		if commitFlags.allowEmpty {
			gitArgs = append(gitArgs, "--allow-empty")
		}
		gitArgs = append(gitArgs, args...)
		return repo.Stream(gitArgs...)
	},
}

// preflight refuses a commit that would stage nothing. git's own refusal prints "nothing added to
// commit but untracked files present" followed by the host repo's entire top level, which reads as
// an attic bug and sends the reader auditing the wrong repo. Say what attic can actually see.
func preflight(hr host.Repo, repo gitwrap.Repo, paths []string) error {
	if len(paths) > 0 || commitFlags.all || commitFlags.allowEmpty {
		return nil
	}
	empty, err := repo.Succeeded("diff", "--cached", "--quiet")
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}

	var b strings.Builder
	b.WriteString("nothing staged for commit")
	scope, err := overlayScope(hr, repo)
	if err != nil {
		return err
	}
	untracked, err := reportableUntracked(repo, scope)
	if err != nil {
		return err
	}
	for _, f := range untracked {
		b.WriteString("\n  untracked: " + f)
	}
	if len(untracked) > 0 {
		b.WriteString("\nstage them with `attic add <path>...`")
	}
	b.WriteString("\ncommit edits to tracked files with `attic commit -a`")
	return errors.New(b.String())
}

func init() {
	commitCmd.Flags().StringVarP(&commitFlags.message, "message", "m", "", "Commit message.")
	commitCmd.Flags().BoolVarP(&commitFlags.all, "all", "a", false, "Stage modifications to tracked files before committing.")
	commitCmd.Flags().BoolVar(&commitFlags.allowEmpty, "allow-empty", false, "Allow an empty commit.")
	root.AddCommand(commitCmd)
}

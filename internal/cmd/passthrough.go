package cmd

import "github.com/spf13/cobra"

// Each entry registers a thin pass-through to git in the overlay context.
// The DisableFlagParsing trick lets users append any git flags without cobra interpreting them.
//
// integrates marks the two that must not run on an overlay stopped part-way through an operation:
// pull would start a second integration on top of the stopped one, and push would publish whatever
// resolution the stopped one left behind. fetch, log and diff read, so they stay available for
// working out what went wrong.
//
// narrowsFetch marks the two that download refs, and so must scope a mono overlay's refspec first.
// sync and rekey already do; without it here, `attic fetch` on an overlay wired before the refspec
// was scoped still pulls every other project's history into this one's bare.
var passthroughs = []struct {
	use, short   string
	integrates   bool
	narrowsFetch bool
}{
	{use: "push", short: "Push overlay commits (git push).", integrates: true},
	{use: "pull", short: "Pull overlay commits (git pull).", integrates: true, narrowsFetch: true},
	{use: "fetch", short: "Fetch overlay refs (git fetch).", narrowsFetch: true},
	{use: "log", short: "Show overlay history (git log)."},
	{use: "diff", short: "Show overlay diff (git diff)."},
}

func init() {
	for _, p := range passthroughs {
		c := &cobra.Command{
			Use:                p.use,
			Short:              p.short,
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, args []string) error {
				_, repo, err := openOverlay()
				if err != nil {
					return err
				}
				if p.integrates {
					if err := ensureNoSequencer(repo, p.use); err != nil {
						return err
					}
				}
				if p.narrowsFetch {
					if err := narrowMonoFetch(repo); err != nil {
						return err
					}
				}
				return repo.Stream(append([]string{p.use}, args...)...)
			},
		}
		root.AddCommand(c)
	}
}

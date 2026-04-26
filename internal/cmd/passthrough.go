package cmd

import "github.com/spf13/cobra"

// Each entry registers a thin pass-through to git in the overlay context.
// The DisableFlagParsing trick lets users append any git flags without cobra interpreting them.
var passthroughs = []struct {
	use, short string
}{
	{"status", "Show overlay status (git status)."},
	{"push", "Push overlay commits (git push)."},
	{"pull", "Pull overlay commits (git pull)."},
	{"fetch", "Fetch overlay refs (git fetch)."},
	{"log", "Show overlay history (git log)."},
	{"diff", "Show overlay diff (git diff)."},
}

func init() {
	for _, p := range passthroughs {
		p := p
		c := &cobra.Command{
			Use:                p.use,
			Short:              p.short,
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, args []string) error {
				_, repo, err := openOverlay()
				if err != nil {
					return err
				}
				return repo.Stream(append([]string{p.use}, args...)...)
			},
		}
		root.AddCommand(c)
	}
}

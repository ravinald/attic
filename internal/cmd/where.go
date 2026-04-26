package cmd

import (
	"fmt"

	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

var whereFlags struct {
	fp bool
}

var whereCmd = &cobra.Command{
	Use:   "where",
	Short: "Print the overlay's storage path and remote for the current host repo.",
	RunE: func(_ *cobra.Command, _ []string) error {
		hr, err := resolveHost()
		if err != nil {
			return err
		}
		fp := hr.Fingerprint()
		if whereFlags.fp {
			fmt.Println(fp)
			return nil
		}
		bare, err := store.BareDir(fp)
		if err != nil {
			return err
		}
		fmt.Printf("host:    %s\nfp:      %s\nbare:    %s\n", hr.Root, fp, bare)
		if m, err := store.LoadMeta(fp); err == nil {
			if m.Branch != "" {
				fmt.Printf("branch:  %s\n", m.Branch)
			}
			switch {
			case m.Remote != "" && m.Mono:
				fmt.Printf("remote:  %s (mono)\n", m.Remote)
			case m.Remote != "":
				fmt.Printf("remote:  %s\n", m.Remote)
			default:
				fmt.Println("remote:  (none)")
			}
		} else {
			fmt.Println("meta:    (no overlay initialised — run `attic init`)")
		}
		return nil
	},
}

func init() {
	whereCmd.Flags().BoolVar(&whereFlags.fp, "fp", false, "Print only the fingerprint.")
	root.AddCommand(whereCmd)
}

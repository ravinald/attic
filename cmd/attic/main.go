// Command attic tracks files alongside a git repo via a per-host bare overlay.
package main

import (
	"fmt"
	"os"

	"github.com/ravinald/attic/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "attic:", err)
		os.Exit(1)
	}
}

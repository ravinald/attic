package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

// Settable keys, kept as constants so get/set/list share one spelling.
const (
	onDuplicateKey  = "gitignore.on_duplicate"
	statusIgnoreKey = "status.ignore"
)

var configFlags struct {
	global bool
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Get or set attic configuration, globally or per-repo.",
	Long: `Read and write attic settings.

Precedence, highest first: command flag, environment variable, per-repo (the overlay's meta.toml),
global (~/.config/attic/config.toml), built-in default. status.ignore is the exception — its layers
union rather than override, so a per-repo pattern never silences the global list.

Keys:
  gitignore.on_duplicate   off | warn | manage — what 'attic add' does when a path is already
                           ignored by a rule outside attic's managed block (default: warn).
                           off leaves it; warn notes it; manage deletes the redundant outside rule.
  status.ignore            comma-separated glob patterns hidden from the untracked-overlay-files
                           list in 'attic status' and 'attic commit' (default: none). A pattern
                           with no slash matches the basename at any depth (.DS_Store), a trailing
                           slash matches a directory's contents (scratch/), anything else matches
                           the whole host-relative path (docs-internal/*.tmp). Set it empty to clear.`,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Print the effective value of a key.",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		switch args[0] {
		case onDuplicateKey:
			v, err := resolveOnDuplicate("")
			if err != nil {
				return err
			}
			fmt.Println(v)
			return nil
		case statusIgnoreKey:
			for _, p := range resolveStatusIgnore() {
				fmt.Println(p)
			}
			return nil
		}
		return unknownConfigKey(args[0])
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a key for the current repo, or globally with --global.",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		key, val := args[0], args[1]
		switch key {
		case onDuplicateKey:
			if !store.ValidOnDuplicate(val) {
				return fmt.Errorf("invalid value %q for %s: want %s, %s, or %s",
					val, key, store.OnDuplicateOff, store.OnDuplicateWarn, store.OnDuplicateManage)
			}
			return setOnDuplicate(val)
		case statusIgnoreKey:
			pats := store.SplitStatusIgnore(val)
			for _, p := range pats {
				if err := store.ValidStatusIgnorePattern(p); err != nil {
					return fmt.Errorf("invalid value for %s: %w", key, err)
				}
			}
			return setStatusIgnore(pats)
		}
		return unknownConfigKey(key)
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show every layer and the effective value.",
	Args:  cobra.NoArgs,
	RunE:  func(_ *cobra.Command, _ []string) error { return listConfig() },
}

func unknownConfigKey(k string) error {
	return fmt.Errorf("unknown config key %q — known keys: %s, %s", k, onDuplicateKey, statusIgnoreKey)
}

func setOnDuplicate(val string) error {
	if configFlags.global {
		c, err := store.LoadConfig()
		if err != nil {
			return err
		}
		c.Gitignore.OnDuplicate = val
		if err := store.SaveConfig(c); err != nil {
			return err
		}
		p, _ := store.ConfigPath()
		fmt.Printf("attic: %s = %s (global: %s)\n", onDuplicateKey, val, p)
		return nil
	}
	hr, m, err := loadMetaForConfig()
	if err != nil {
		return err
	}
	m.GitignoreOnDuplicate = val
	if err := store.SaveMeta(m); err != nil {
		return err
	}
	fmt.Printf("attic: %s = %s (repo: %s)\n", onDuplicateKey, val, hr.Root)
	return nil
}

func setStatusIgnore(pats []string) error {
	if configFlags.global {
		c, err := store.LoadConfig()
		if err != nil {
			return err
		}
		c.Status.Ignore = pats
		if err := store.SaveConfig(c); err != nil {
			return err
		}
		p, _ := store.ConfigPath()
		fmt.Printf("attic: %s = %s (global: %s)\n", statusIgnoreKey, orNone(pats), p)
		return nil
	}
	hr, m, err := loadMetaForConfig()
	if err != nil {
		return err
	}
	m.StatusIgnore = pats
	if err := store.SaveMeta(m); err != nil {
		return err
	}
	fmt.Printf("attic: %s = %s (repo: %s)\n", statusIgnoreKey, orNone(pats), hr.Root)
	return nil
}

// loadMetaForConfig resolves the current host repo's overlay metadata for a per-repo write.
func loadMetaForConfig() (host.Repo, store.Meta, error) {
	hr, err := resolveHost()
	if err != nil {
		return hr, store.Meta{}, err
	}
	m, err := store.LoadMeta(hr.Fingerprint())
	if err != nil {
		return hr, store.Meta{}, fmt.Errorf("no overlay for %s — run `attic init` first, or set it with --global", hr.Root)
	}
	return hr, m, nil
}

func listConfig() error {
	global, err := store.LoadConfig()
	if err != nil {
		return err
	}
	env := envOnDuplicate()
	perRepo := onDuplicatePerRepo()
	eff, err := store.ResolveOnDuplicate("", env, perRepo, global)
	if err != nil {
		return err
	}
	fmt.Println(onDuplicateKey)
	fmt.Printf("  env:       %s\n", orUnset(env))
	fmt.Printf("  per-repo:  %s\n", orUnset(perRepo))
	fmt.Printf("  global:    %s\n", orUnset(global.Gitignore.OnDuplicate))
	fmt.Printf("  effective: %s\n", eff)

	fmt.Println(statusIgnoreKey + "  (layers union)")
	fmt.Printf("  env:       %s\n", orNone(store.SplitStatusIgnore(os.Getenv(statusIgnoreEnv))))
	fmt.Printf("  per-repo:  %s\n", orNone(statusIgnorePerRepo()))
	fmt.Printf("  global:    %s\n", orNone(global.Status.Ignore))
	fmt.Printf("  effective: %s\n", orNone(resolveStatusIgnore()))
	return nil
}

func orUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func orNone(p []string) string {
	if len(p) == 0 {
		return "(none)"
	}
	return strings.Join(p, ", ")
}

func init() {
	configSetCmd.Flags().BoolVar(&configFlags.global, "global", false, "Write to the global config instead of the current repo.")
	configCmd.AddCommand(configGetCmd, configSetCmd, configListCmd)
	root.AddCommand(configCmd)
}

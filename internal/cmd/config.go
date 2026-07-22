package cmd

import (
	"fmt"

	"github.com/ravinald/attic/internal/store"
	"github.com/spf13/cobra"
)

// onDuplicateKey is the only settable key for now; kept as a constant so get/set/list share one spelling.
const onDuplicateKey = "gitignore.on_duplicate"

var configFlags struct {
	global bool
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Get or set attic configuration, globally or per-repo.",
	Long: `Read and write attic settings.

Precedence, highest first: command flag, environment variable, per-repo (the overlay's meta.toml),
global (~/.config/attic/config.toml), built-in default.

Keys:
  gitignore.on_duplicate   off | warn | manage — what 'attic add' does when a path is already
                           ignored by a rule outside attic's managed block (default: warn).
                           off leaves it; warn notes it; manage deletes the redundant outside rule.`,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Print the effective value of a key.",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if args[0] != onDuplicateKey {
			return unknownConfigKey(args[0])
		}
		v, err := resolveOnDuplicate("")
		if err != nil {
			return err
		}
		fmt.Println(v)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a key for the current repo, or globally with --global.",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		key, val := args[0], args[1]
		if key != onDuplicateKey {
			return unknownConfigKey(key)
		}
		if !store.ValidOnDuplicate(val) {
			return fmt.Errorf("invalid value %q for %s: want %s, %s, or %s",
				val, key, store.OnDuplicateOff, store.OnDuplicateWarn, store.OnDuplicateManage)
		}
		if configFlags.global {
			return setOnDuplicateGlobal(val)
		}
		return setOnDuplicatePerRepo(val)
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show every layer and the effective value.",
	Args:  cobra.NoArgs,
	RunE:  func(_ *cobra.Command, _ []string) error { return listConfig() },
}

func unknownConfigKey(k string) error {
	return fmt.Errorf("unknown config key %q — known keys: %s", k, onDuplicateKey)
}

func setOnDuplicateGlobal(val string) error {
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

func setOnDuplicatePerRepo(val string) error {
	hr, err := resolveHost()
	if err != nil {
		return err
	}
	m, err := store.LoadMeta(hr.Fingerprint())
	if err != nil {
		return fmt.Errorf("no overlay for %s — run `attic init` first, or set it with --global", hr.Root)
	}
	m.GitignoreOnDuplicate = val
	if err := store.SaveMeta(m); err != nil {
		return err
	}
	fmt.Printf("attic: %s = %s (repo: %s)\n", onDuplicateKey, val, hr.Root)
	return nil
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
	return nil
}

func orUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func init() {
	configSetCmd.Flags().BoolVar(&configFlags.global, "global", false, "Write to the global config instead of the current repo.")
	configCmd.AddCommand(configGetCmd, configSetCmd, configListCmd)
	root.AddCommand(configCmd)
}

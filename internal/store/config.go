package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// OnDuplicate modes govern what `attic add` does when a path it stages is already ignored by a rule
// outside attic's managed block.
const (
	OnDuplicateOff    = "off"    // add to the block, leave the outside rule untouched
	OnDuplicateWarn   = "warn"   // add to the block, print a hint about the redundant rule
	OnDuplicateManage = "manage" // add to the block and delete the redundant outside rule
)

// DefaultOnDuplicate is the built-in policy when no layer sets one.
const DefaultOnDuplicate = OnDuplicateWarn

// ValidOnDuplicate reports whether m is a known on-duplicate mode.
func ValidOnDuplicate(m string) bool {
	switch m {
	case OnDuplicateOff, OnDuplicateWarn, OnDuplicateManage:
		return true
	}
	return false
}

// Config is attic's machine-wide configuration, stored at ~/.config/attic/config.toml.
type Config struct {
	Gitignore GitignoreConfig `toml:"gitignore"`
	Status    StatusConfig    `toml:"status"`
}

// GitignoreConfig holds host .gitignore management policy.
type GitignoreConfig struct {
	OnDuplicate string `toml:"on_duplicate,omitempty"`
}

// StatusConfig holds reporting policy for `attic status` and `attic commit`.
type StatusConfig struct {
	Ignore []string `toml:"ignore,omitempty"`
}

// ConfigPath returns the global config file path.
func ConfigPath() (string, error) {
	c, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "config.toml"), nil
}

// LoadConfig reads the global config. A missing file yields the zero Config, which is valid.
func LoadConfig() (Config, error) {
	var c Config
	p, err := ConfigPath()
	if err != nil {
		return c, err
	}
	if _, err := toml.DecodeFile(p, &c); err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, fmt.Errorf("store: load config %s: %w", p, err)
	}
	return c, nil
}

// SaveConfig writes the global config atomically.
func SaveConfig(c Config) error {
	p, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("store: mkdir config: %w", err)
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("store: create config tmp: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(c); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("store: encode config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("store: close config tmp: %w", err)
	}
	return os.Rename(tmp, p)
}

// ResolveOnDuplicate applies precedence flag > env > per-repo > global > default and validates the
// winner. An empty string means "unset" at that layer.
func ResolveOnDuplicate(flag, env, perRepo string, global Config) (string, error) {
	for _, v := range []string{flag, env, perRepo, global.Gitignore.OnDuplicate} {
		if v == "" {
			continue
		}
		if !ValidOnDuplicate(v) {
			return "", fmt.Errorf("invalid on_duplicate %q: want %s, %s, or %s",
				v, OnDuplicateOff, OnDuplicateWarn, OnDuplicateManage)
		}
		return v, nil
	}
	return DefaultOnDuplicate, nil
}

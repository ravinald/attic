package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ConfigHome returns attic's config root, e.g. ~/.config/attic, honouring XDG_CONFIG_HOME.
func ConfigHome() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "attic"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "attic"), nil
}

// OverridesPath returns the local label-overrides file. These names are display-only and never leave
// the machine — the pushed map on the mono remote stays the shared source of truth.
func OverridesPath() (string, error) {
	c, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "overrides.toml"), nil
}

type overridesDoc struct {
	Overrides map[string]string `toml:"overrides"`
}

// LoadOverrides reads the fingerprint→label local overrides. A missing file is an empty set.
func LoadOverrides() (map[string]string, error) {
	p, err := OverridesPath()
	if err != nil {
		return nil, err
	}
	var d overridesDoc
	if _, err := toml.DecodeFile(p, &d); err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("store: load overrides %s: %w", p, err)
	}
	if d.Overrides == nil {
		d.Overrides = map[string]string{}
	}
	return d.Overrides, nil
}

// ClearOverrides removes every local override on this machine by deleting the overrides file.
func ClearOverrides() error {
	p, err := OverridesPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("store: remove overrides %s: %w", p, err)
	}
	return nil
}

// SetOverride sets fp's local label, or removes it when label is empty. Writes atomically.
func SetOverride(fp, label string) error {
	cur, err := LoadOverrides()
	if err != nil {
		return err
	}
	if label == "" {
		delete(cur, fp)
	} else {
		cur[fp] = label
	}
	p, err := OverridesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("store: mkdir config: %w", err)
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("store: create overrides tmp: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(overridesDoc{Overrides: cur}); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("store: encode overrides: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("store: close overrides tmp: %w", err)
	}
	return os.Rename(tmp, p)
}

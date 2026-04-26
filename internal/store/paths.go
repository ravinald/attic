// Package store resolves on-disk locations for attic's per-overlay state and reads/writes overlay metadata.
package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataHome returns the attic data root, e.g. ~/.local/share/attic.
func DataHome() (string, error) {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "attic"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "attic"), nil
}

// RepoDir returns the per-host-repo storage dir for a fingerprint.
func RepoDir(fp string) (string, error) {
	base, err := DataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "repos", fp), nil
}

// BareDir returns the bare git path inside the per-repo storage dir.
func BareDir(fp string) (string, error) {
	rd, err := RepoDir(fp)
	if err != nil {
		return "", err
	}
	return filepath.Join(rd, "attic.git"), nil
}

// MetaPath returns the meta.toml path inside the per-repo storage dir.
func MetaPath(fp string) (string, error) {
	rd, err := RepoDir(fp)
	if err != nil {
		return "", err
	}
	return filepath.Join(rd, "meta.toml"), nil
}

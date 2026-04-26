package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Meta is the per-overlay metadata persisted alongside the bare repo.
type Meta struct {
	Fingerprint string    `toml:"fingerprint"`
	HostRoot    string    `toml:"host_root"`
	HostName    string    `toml:"host_name"`
	OriginURL   string    `toml:"origin_url,omitempty"`
	Remote      string    `toml:"remote,omitempty"`
	Branch      string    `toml:"branch,omitempty"` // "main" for per-repo, "host/<fp>" for mono
	Mono        bool      `toml:"mono,omitempty"`   // true if remote is a shared mono repo
	CreatedAt   time.Time `toml:"created_at"`
}

// LoadMeta reads meta.toml from the per-repo storage dir.
func LoadMeta(fp string) (Meta, error) {
	var m Meta
	p, err := MetaPath(fp)
	if err != nil {
		return m, err
	}
	if _, err := toml.DecodeFile(p, &m); err != nil {
		return m, fmt.Errorf("store: load meta %s: %w", p, err)
	}
	return m, nil
}

// SaveMeta writes meta.toml atomically.
func SaveMeta(m Meta) error {
	p, err := MetaPath(m.Fingerprint)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("store: mkdir for meta: %w", err)
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("store: create meta tmp: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(m); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("store: encode meta: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("store: close meta tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("store: rename meta: %w", err)
	}
	return nil
}

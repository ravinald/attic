package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Label provenance values for Meta.LabelSource.
const (
	LabelSourceOrigin = "origin" // derived from the host repo's origin remote
	LabelSourceManual = "manual" // set by hand via `attic label set`
)

// Meta is the per-overlay metadata persisted alongside the bare repo.
type Meta struct {
	Fingerprint string    `toml:"fingerprint"`
	HostRoot    string    `toml:"host_root"`
	HostName    string    `toml:"host_name"`
	Label       string    `toml:"label,omitempty"`        // user-editable display name; falls back to HostName
	LabelSource string    `toml:"label_source,omitempty"` // "origin" = auto-derived from origin_url, "manual" = user-set
	OriginURL   string    `toml:"origin_url,omitempty"`
	Remote      string    `toml:"remote,omitempty"`
	Branch      string    `toml:"branch,omitempty"` // "main" for per-repo, "repo/<fp>" for mono
	Mono        bool      `toml:"mono,omitempty"`   // true if remote is a shared mono repo
	CreatedAt   time.Time `toml:"created_at"`

	GitignoreOnDuplicate string   `toml:"gitignore_on_duplicate,omitempty"` // per-repo override of the global on_duplicate policy
	StatusIgnore         []string `toml:"status_ignore,omitempty"`          // per-repo patterns, unioned with the global status.ignore
}

// DisplayLabel returns the user-set Label, or HostName as a fallback.
func (m Meta) DisplayLabel() string {
	if m.Label != "" {
		return m.Label
	}
	return m.HostName
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

// EnumerateMetas scans ~/.local/share/attic/repos/ and returns one Meta per readable meta.toml.
// Unreadable or malformed entries are skipped silently — a single corrupt overlay must not break listing.
func EnumerateMetas() ([]Meta, error) {
	base, err := DataHome()
	if err != nil {
		return nil, err
	}
	reposDir := filepath.Join(base, "repos")
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: read repos dir %s: %w", reposDir, err)
	}
	out := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := LoadMeta(e.Name())
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// FindMetasByHostRoot returns every overlay whose recorded host_root is root. Fingerprints are
// derived from the host's root commit, so a history rewrite leaves storage registered to this work
// tree under a key the repo no longer hashes to; this is the reverse lookup that finds it. More than
// one match means two overlays claim the same work tree, which a caller must resolve rather than
// guess at.
func FindMetasByHostRoot(root string) ([]Meta, error) {
	metas, err := EnumerateMetas()
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, m := range metas {
		if m.HostRoot == root {
			out = append(out, m)
		}
	}
	return out, nil
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

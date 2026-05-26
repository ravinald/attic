package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDisplayLabelFallsBackToHostName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, label, host, want string
	}{
		{"label set", "wifimgr-personal", "wifimgr", "wifimgr-personal"},
		{"label empty", "", "wifimgr", "wifimgr"},
		{"both empty", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := Meta{Label: c.label, HostName: c.host}
			if got := m.DisplayLabel(); got != c.want {
				t.Fatalf("DisplayLabel()=%q want %q", got, c.want)
			}
		})
	}
}

func TestEnumerateMetas(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	// Seed two valid overlays.
	for _, m := range []Meta{
		{Fingerprint: "aaaaaaaaaaaa", HostRoot: "/x/alpha", HostName: "alpha", CreatedAt: time.Now().UTC()},
		{Fingerprint: "bbbbbbbbbbbb", HostRoot: "/x/beta", HostName: "beta", Label: "beta-pretty", CreatedAt: time.Now().UTC()},
	} {
		if err := SaveMeta(m); err != nil {
			t.Fatalf("seed %s: %v", m.Fingerprint, err)
		}
	}

	// Seed a dir without meta.toml to confirm we skip silently.
	junkDir := filepath.Join(dir, "attic", "repos", "junk")
	if err := os.MkdirAll(junkDir, 0o755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}

	// Seed a dir with malformed meta.toml to confirm we skip silently.
	badDir := filepath.Join(dir, "attic", "repos", "cccccccccccc")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("mkdir bad: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "meta.toml"), []byte("this is not toml = ["), 0o644); err != nil {
		t.Fatalf("write bad meta: %v", err)
	}

	metas, err := EnumerateMetas()
	if err != nil {
		t.Fatalf("EnumerateMetas: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("EnumerateMetas returned %d metas, want 2: %+v", len(metas), metas)
	}
	seen := map[string]string{}
	for _, m := range metas {
		seen[m.Fingerprint] = m.DisplayLabel()
	}
	if seen["aaaaaaaaaaaa"] != "alpha" {
		t.Errorf("alpha label = %q, want %q", seen["aaaaaaaaaaaa"], "alpha")
	}
	if seen["bbbbbbbbbbbb"] != "beta-pretty" {
		t.Errorf("beta label = %q, want %q", seen["bbbbbbbbbbbb"], "beta-pretty")
	}
}

func TestEnumerateMetasMissingDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	metas, err := EnumerateMetas()
	if err != nil {
		t.Fatalf("EnumerateMetas on empty home: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("expected 0 metas, got %d", len(metas))
	}
}

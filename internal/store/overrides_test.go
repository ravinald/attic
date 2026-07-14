package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverridesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	ov, err := LoadOverrides()
	if err != nil {
		t.Fatalf("LoadOverrides (empty): %v", err)
	}
	if len(ov) != 0 {
		t.Fatalf("expected empty overrides, got %v", ov)
	}

	if err := SetOverride("8b88ecad3aa9", "capy.cat"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if err := SetOverride("67c031190db7", "bodega"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "attic", "overrides.toml")); err != nil {
		t.Fatalf("overrides file not written: %v", err)
	}

	ov, _ = LoadOverrides()
	if ov["8b88ecad3aa9"] != "capy.cat" || ov["67c031190db7"] != "bodega" {
		t.Fatalf("round-trip mismatch: %v", ov)
	}

	if err := SetOverride("8b88ecad3aa9", ""); err != nil {
		t.Fatalf("SetOverride unset: %v", err)
	}
	ov, _ = LoadOverrides()
	if _, ok := ov["8b88ecad3aa9"]; ok {
		t.Fatalf("expected 8b88ecad3aa9 removed, got %v", ov)
	}
	if ov["67c031190db7"] != "bodega" {
		t.Fatalf("unset dropped the wrong key: %v", ov)
	}
}

func TestClearOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := ClearOverrides(); err != nil {
		t.Fatalf("ClearOverrides on empty: %v", err)
	}
	if err := SetOverride("abc", "x"); err != nil {
		t.Fatal(err)
	}
	if err := ClearOverrides(); err != nil {
		t.Fatalf("ClearOverrides: %v", err)
	}
	ov, _ := LoadOverrides()
	if len(ov) != 0 {
		t.Fatalf("expected no overrides after clear, got %v", ov)
	}
}

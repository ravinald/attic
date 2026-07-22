package ignore

import (
	"path/filepath"
	"strings"
	"testing"
)

const blockHeader = BeginPrefix + " — managed by `attic`, do not edit between markers\n"

func TestFindDuplicatesSlashInsensitive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	writeFile(t, p, "/bin/\n/docs-internal/\n"+blockHeader+"docs-internal\n"+EndPrefix+"\n")

	dups, err := FindDuplicates(p, []string{"docs-internal"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate, got %d: %v", len(dups), dups)
	}
	if dups[0].Line != 2 || dups[0].Text != "/docs-internal/" || dups[0].Path != "docs-internal" {
		t.Fatalf("unexpected duplicate: %+v", dups[0])
	}
}

func TestFindDuplicatesIgnoresBlockAndGlobs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	// A glob match and the in-block entry itself must not count as duplicates.
	writeFile(t, p, "docs-*\n"+blockHeader+"docs-internal\n"+EndPrefix+"\n")

	dups, err := FindDuplicates(p, []string{"docs-internal"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(dups) != 0 {
		t.Fatalf("expected no duplicates, got %v", dups)
	}
}

func TestFindDuplicatesMissingFile(t *testing.T) {
	dups, err := FindDuplicates(filepath.Join(t.TempDir(), "nope"), []string{"x"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if dups != nil {
		t.Fatalf("expected nil, got %v", dups)
	}
}

func TestDropOutsideRemovesOnlyOutsideLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	writeFile(t, p, "/bin/\n/docs-internal/\n"+blockHeader+"docs-internal\n"+EndPrefix+"\ntrailing\n")

	b, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	b.DropOutside("/docs-internal/")
	if err := b.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := read(t, p)
	if strings.Contains(got, "/docs-internal/") {
		t.Fatalf("outside duplicate not removed: %q", got)
	}
	// The block's own entry, and unrelated lines, must survive.
	for _, keep := range []string{"/bin/", "trailing", "\ndocs-internal\n"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("dropped too much, missing %q: %q", keep, got)
		}
	}
}

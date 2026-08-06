package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(b.Lines) != 0 {
		t.Fatalf("expected empty block, got %v", b.Lines)
	}
}

func TestSaveCreatesFileWithBlock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	b, _ := Load(p)
	b.Add("docs-internal/", "CLAUDE.md")
	if err := b.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := read(t, p)
	if !strings.Contains(got, BeginPrefix) || !strings.Contains(got, EndPrefix) {
		t.Fatalf("missing markers: %s", got)
	}
	if !strings.Contains(got, "docs-internal/") || !strings.Contains(got, "CLAUDE.md") {
		t.Fatalf("missing entries: %s", got)
	}
}

func TestSplicePreservesSurroundingContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	writeFile(t, p, "/bin/\n*.test\n")
	b, _ := Load(p)
	b.Add("CLAUDE.md")
	if err := b.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := read(t, p)
	if !strings.HasPrefix(got, "/bin/\n*.test\n") {
		t.Fatalf("clobbered surrounding content: %s", got)
	}
	if !strings.Contains(got, "CLAUDE.md") {
		t.Fatalf("missing entry: %s", got)
	}
}

func TestSpliceUpdatesExistingBlock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	writeFile(t, p, "/bin/\n"+
		BeginPrefix+" — managed by `attic`, do not edit between markers\n"+
		"old.md\n"+
		EndPrefix+"\n"+
		"trailing.txt\n")
	b, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !b.Has("old.md") {
		t.Fatalf("expected to load old.md")
	}
	b.Remove("old.md")
	b.Add("new.md")
	if err := b.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := read(t, p)
	if strings.Contains(got, "old.md") {
		t.Fatalf("old entry not removed: %s", got)
	}
	if !strings.Contains(got, "new.md") {
		t.Fatalf("new entry not added: %s", got)
	}
	if !strings.Contains(got, "trailing.txt") {
		t.Fatalf("trailing content lost: %s", got)
	}
	if !strings.HasPrefix(got, "/bin/\n") {
		t.Fatalf("leading content lost: %s", got)
	}
}

func TestRoundtripIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	b, _ := Load(p)
	b.Add("a", "b", "c")
	if err := b.Save(); err != nil {
		t.Fatalf("save1: %v", err)
	}
	first := read(t, p)
	b2, _ := Load(p)
	if err := b2.Save(); err != nil {
		t.Fatalf("save2: %v", err)
	}
	if read(t, p) != first {
		t.Fatalf("roundtrip not idempotent")
	}
}

func TestAddDeduplicates(t *testing.T) {
	b := Block{}
	b.Add("a", "a", "b", "a")
	if len(b.Lines) != 2 {
		t.Fatalf("expected 2 unique lines, got %d: %v", len(b.Lines), b.Lines)
	}
}

func TestCovers(t *testing.T) {
	b := Block{Lines: []string{"docs-internal", ".drover.toml", "logs/*.json", "build/output"}}

	cases := []struct {
		name string
		path string
		want string // "" = not covered
	}{
		{"child of a directory entry", "docs-internal/CHANGELOG.md", "docs-internal"},
		{"deep child", "docs-internal/baselines/a.json", "docs-internal"},
		{"nested entry child", "build/output/bin", "build/output"},
		{"exact match is Has, not Covers", "docs-internal", ""},
		{"exact file entry", ".drover.toml", ""},
		{"glob entries never cover", "logs/a.json", ""},
		{"unrelated path", "internal/main.go", ""},
		{"prefix but not a path boundary", "docs-internal-scratch/a.md", ""},
		{"empty path", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			by, ok := b.Covers(tc.path)
			if tc.want == "" {
				if ok {
					t.Errorf("Covers(%q) = %q, want not covered", tc.path, by)
				}
				return
			}
			if !ok || by != tc.want {
				t.Errorf("Covers(%q) = (%q, %v), want (%q, true)", tc.path, by, ok, tc.want)
			}
		})
	}
}

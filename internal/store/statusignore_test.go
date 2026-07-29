package store

import (
	"reflect"
	"testing"
)

func TestStatusIgnoredMatching(t *testing.T) {
	cases := []struct {
		name, rel, pattern string
		want               bool
	}{
		{"basename at depth", "docs-internal/images/.DS_Store", ".DS_Store", true},
		{"basename at root", ".DS_Store", ".DS_Store", true},
		{"basename glob", "docs-internal/notes.tmp", "*.tmp", true},
		{"basename does not match a path", "docs-internal/a.md", "a.md", true},
		{"non-match", "docs-internal/a.md", ".DS_Store", false},

		{"globstar synonym", "docs-internal/images/.DS_Store", "**/.DS_Store", true},

		{"full path", "docs-internal/notes.tmp", "docs-internal/*.tmp", true},
		{"full path is not a suffix match", "a/docs-internal/notes.tmp", "docs-internal/*.tmp", false},
		{"full path wrong dir", "other/notes.tmp", "docs-internal/*.tmp", false},

		{"bare dir at depth", "docs-internal/scratch/a.md", "scratch/", true},
		{"bare dir nested deeper", "docs-internal/scratch/x/a.md", "scratch/", true},
		{"bare dir does not match a file of that name", "docs-internal/scratch", "scratch/", false},
		{"rooted dir", "docs-internal/scratch/a.md", "docs-internal/scratch/", true},
		{"rooted dir wrong prefix", "other/scratch/a.md", "docs-internal/scratch/", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := StatusIgnored(c.rel, c.pattern)
			if err != nil {
				t.Fatalf("StatusIgnored(%q, %q): %v", c.rel, c.pattern, err)
			}
			if got != c.want {
				t.Errorf("StatusIgnored(%q, %q) = %v, want %v", c.rel, c.pattern, got, c.want)
			}
		})
	}
}

func TestValidStatusIgnorePattern(t *testing.T) {
	for _, p := range []string{".DS_Store", "*.tmp", "docs-internal/*.tmp", "scratch/", "**/.DS_Store"} {
		if err := ValidStatusIgnorePattern(p); err != nil {
			t.Errorf("ValidStatusIgnorePattern(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range []string{"", "   ", "[", "a[b"} {
		if err := ValidStatusIgnorePattern(p); err == nil {
			t.Errorf("ValidStatusIgnorePattern(%q) = nil, want an error", p)
		}
	}
}

// TestResolveStatusIgnoreUnions is the whole point of the key: a per-repo pattern must add to the
// global list, never replace it, or setting one repo-local pattern silently unhides .DS_Store.
func TestResolveStatusIgnoreUnions(t *testing.T) {
	global := Config{Status: StatusConfig{Ignore: []string{".DS_Store"}}}
	got := ResolveStatusIgnore([]string{"*.swp"}, []string{"scratch/", ".DS_Store"}, global)
	want := []string{"*.swp", "scratch/", ".DS_Store"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveStatusIgnore = %q, want %q", got, want)
	}
}

func TestResolveStatusIgnoreEmpty(t *testing.T) {
	if got := ResolveStatusIgnore(nil, nil, Config{}); len(got) != 0 {
		t.Errorf("ResolveStatusIgnore = %q, want none", got)
	}
}

func TestFilterStatusIgnored(t *testing.T) {
	paths := []string{
		"docs-internal/.DS_Store",
		"docs-internal/images/.DS_Store",
		"docs-internal/verdicts/F07-the-front-door.md",
		"docs-internal/scratch/draft.md",
	}
	kept, malformed := FilterStatusIgnored(paths, []string{".DS_Store", "scratch/"})
	want := []string{"docs-internal/verdicts/F07-the-front-door.md"}
	if !reflect.DeepEqual(kept, want) {
		t.Errorf("kept = %q, want %q", kept, want)
	}
	if len(malformed) != 0 {
		t.Errorf("malformed = %q, want none", malformed)
	}
}

// TestFilterStatusIgnoredReportsBadPatternOnce keeps a typo visible without one warning per file.
func TestFilterStatusIgnoredReportsBadPatternOnce(t *testing.T) {
	kept, malformed := FilterStatusIgnored([]string{"a.md", "b.md"}, []string{"["})
	if want := []string{"a.md", "b.md"}; !reflect.DeepEqual(kept, want) {
		t.Errorf("kept = %q, want %q — a bad pattern must not hide files", kept, want)
	}
	if want := []string{"["}; !reflect.DeepEqual(malformed, want) {
		t.Errorf("malformed = %q, want %q", malformed, want)
	}
}

func TestFilterStatusIgnoredNoPatternsIsIdentity(t *testing.T) {
	paths := []string{"a.md", "b.md"}
	kept, _ := FilterStatusIgnored(paths, nil)
	if !reflect.DeepEqual(kept, paths) {
		t.Errorf("kept = %q, want %q", kept, paths)
	}
}

func TestSplitStatusIgnore(t *testing.T) {
	got := SplitStatusIgnore(" .DS_Store , scratch/ ,, ")
	want := []string{".DS_Store", "scratch/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitStatusIgnore = %q, want %q", got, want)
	}
	if got := SplitStatusIgnore(""); len(got) != 0 {
		t.Errorf("SplitStatusIgnore(\"\") = %q, want none", got)
	}
}

func TestStatusIgnoreRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveConfig(Config{Status: StatusConfig{Ignore: []string{".DS_Store", "scratch/"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := []string{".DS_Store", "scratch/"}; !reflect.DeepEqual(c.Status.Ignore, want) {
		t.Fatalf("round-trip lost value: %+v", c)
	}
}

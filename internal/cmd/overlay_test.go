package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
)

func TestEnsureOverlayExcludeIsIdempotent(t *testing.T) {
	bare := t.TempDir()
	path := filepath.Join(bare, "info", "exclude")

	for range 2 {
		if err := ensureOverlayExclude(bare); err != nil {
			t.Fatalf("ensureOverlayExclude: %v", err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	want := "# BEGIN attic — managed by `attic`, do not edit between markers\n/*\n# END attic\n"
	if string(got) != want {
		t.Errorf("exclude =\n%q\nwant\n%q", got, want)
	}
}

func TestEnsureOverlayExcludePreservesGitsBoilerplate(t *testing.T) {
	bare := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bare, "info", "exclude")
	if err := os.WriteFile(path, []byte("# comment from git init\n*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOverlayExclude(bare); err != nil {
		t.Fatalf("ensureOverlayExclude: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# comment from git init\n*.tmp\n# BEGIN attic — managed by `attic`, do not edit between markers\n/*\n# END attic\n"
	if string(got) != want {
		t.Errorf("exclude =\n%q\nwant\n%q", got, want)
	}
}

func TestMachineReadable(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"plain", nil, false},
		{"short", []string{"-s"}, true},
		{"long short", []string{"--short"}, true},
		{"porcelain v2", []string{"--porcelain=v2"}, true},
		{"nul separated", []string{"-z"}, true},
		{"pathspec named -s after --", []string{"--", "-s"}, false},
		{"unrelated flag", []string{"--branch"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := machineReadable(tc.args); got != tc.want {
				t.Errorf("machineReadable(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestOverlayHidesHostFiles is the regression: an overlay's work tree is the whole host repo, so
// without the exclude every host file reads as untracked and buries the overlay's own changes.
func TestOverlayHidesHostFiles(t *testing.T) {
	hr, repo := newOverlayFixture(t)

	writeFile(t, hr.Root, "docs-internal/b.md", "new")
	out, err := repo.Run("status", "--porcelain")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if out != "" {
		t.Errorf("host files leaked into overlay status:\n%s", out)
	}

	scope, err := overlayScope(hr, repo)
	if err != nil {
		t.Fatalf("overlayScope: %v", err)
	}
	if want := []string{"docs-internal"}; !reflect.DeepEqual(scope, want) {
		t.Fatalf("overlayScope = %q, want %q", scope, want)
	}

	untracked, err := untrackedOverlayFiles(repo, scope)
	if err != nil {
		t.Fatalf("untrackedOverlayFiles: %v", err)
	}
	if want := []string{"docs-internal/b.md"}; !reflect.DeepEqual(untracked, want) {
		t.Errorf("untrackedOverlayFiles = %q, want %q", untracked, want)
	}
}

// TestReportableUntrackedAppliesStatusIgnore covers the seam status.ignore exists for: every file
// under overlay scope is ignored by construction, so git's own exclude machinery can never filter
// this list — only attic can, after the ls-files call.
func TestReportableUntrackedAppliesStatusIgnore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(statusIgnoreEnv, ".DS_Store, scratch/")

	hr, repo := newOverlayFixture(t)
	writeFile(t, hr.Root, "docs-internal/.DS_Store", "finder")
	writeFile(t, hr.Root, "docs-internal/images/.DS_Store", "finder")
	writeFile(t, hr.Root, "docs-internal/scratch/draft.md", "draft")
	writeFile(t, hr.Root, "docs-internal/verdict.md", "real work")

	scope, err := overlayScope(hr, repo)
	if err != nil {
		t.Fatalf("overlayScope: %v", err)
	}

	all, err := untrackedOverlayFiles(repo, scope)
	if err != nil {
		t.Fatalf("untrackedOverlayFiles: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("unfiltered = %q, want all 4 files — the fixture is wrong, not the filter", all)
	}

	got, err := reportableUntracked(repo, scope)
	if err != nil {
		t.Fatalf("reportableUntracked: %v", err)
	}
	if want := []string{"docs-internal/verdict.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reportableUntracked = %q, want %q", got, want)
	}
}

// newOverlayFixture builds a host repo with an attic-managed .gitignore block plus a bare overlay
// tracking docs-internal, matching what `attic init` + `attic add docs-internal` produce.
func newOverlayFixture(t *testing.T) (host.Repo, gitwrap.Repo) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(t.TempDir(), "attic.git")

	writeFile(t, root, "cmd/main.go", "package main")
	writeFile(t, root, "README.md", "# host")
	writeFile(t, root, ".gitignore", "# BEGIN attic\ndocs-internal\n# END attic\n")
	writeFile(t, root, "docs-internal/a.md", "doc")

	git(t, root, "init", "-q", ".")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "init")

	if err := (gitwrap.Repo{}).Stream("init", "--bare", "-q", "-b", "main", bare); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if err := ensureOverlayExclude(bare); err != nil {
		t.Fatalf("ensureOverlayExclude: %v", err)
	}
	repo := gitwrap.Repo{GitDir: bare, WorkTree: root}
	if err := repo.Stream("add", "--force", "--", "docs-internal"); err != nil {
		t.Fatalf("overlay add: %v", err)
	}
	if err := repo.Stream("commit", "-qm", "overlay"); err != nil {
		t.Fatalf("overlay commit: %v", err)
	}
	return host.Repo{Root: root}, repo
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

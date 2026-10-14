package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMain points both XDG roots at a scratch directory before any test runs. Commands under test
// write machine config as a side effect (clone and init record the default mono remote), and a test
// that sets only XDG_DATA_HOME would otherwise write that into the developer's real config.
func TestMain(m *testing.M) {
	scratch, err := os.MkdirTemp("", "attic-cmd-test-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		if err := os.Setenv(v, filepath.Join(scratch, v)); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(scratch)
	os.Exit(code)
}

// TestRelativiseToHostResolvesNearestExistingAncestor pins the macOS shape of this bug: the host root
// is canonicalised at detection (/private/var/...) while the cwd a user types from is not (/var/...).
// EvalSymlinks needs a path to exist, so a path being registered before it is created kept the
// uncanonical form, and filepath.Rel between the two forms yielded "../..." — reporting a path plainly
// inside the repo as outside it. The symlink here is explicit so the test reproduces off macOS too.
func TestRelativiseToHostResolvesNearestExistingAncestor(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	if err := os.MkdirAll(filepath.Join(realDir, "docs-internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	hostRoot, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"existing dir", []string{"docs-internal"}, []string{"docs-internal"}},
		{"file that does not exist yet", []string{"docs-internal/not-yet.md"}, []string{"docs-internal/not-yet.md"}},
		{"dir that does not exist yet", []string{"notes"}, []string{"notes"}},
		{"nested missing ancestors", []string{"a/b/c.md"}, []string{"a/b/c.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := relativiseToHost(hostRoot, tc.args)
			if err != nil {
				t.Fatalf("relativiseToHost(%q) = %v", tc.args, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("relativiseToHost(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// A path genuinely outside the repo must still be refused; the fix must not widen the guard.
func TestRelativiseToHostStillRefusesOutsidePaths(t *testing.T) {
	base := t.TempDir()
	hostRoot := filepath.Join(base, "host")
	if err := os.MkdirAll(hostRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(hostRoot)
	for _, arg := range []string{"../escape.md", "../../escape.md", filepath.Join(base, "sibling.md")} {
		if _, err := relativiseToHost(hostRoot, []string{arg}); err == nil {
			t.Errorf("relativiseToHost(%q) accepted a path outside the repo", arg)
		}
	}
}

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ravinald/attic/internal/gitwrap"
	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/store"
)

// TestDoctorFlagsOrphanedFingerprint covers the machine-wide half of the diagnosis: an orphan is only
// self-evident inside the affected repo, and doctor is what finds one in a repo nobody has opened.
func TestDoctorFlagsOrphanedFingerprint(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	hostDir := t.TempDir()
	writeFile(t, hostDir, "README.md", "# host")
	git(t, hostDir, "init", "-q", ".")
	git(t, hostDir, "add", "-A")
	git(t, hostDir, "commit", "-qm", "init")
	hr, err := host.Detect(hostDir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}

	// A fingerprint the repo cannot hash to, standing in for the key a rewrite left behind.
	const stale = "deadbeefdead"
	bare, err := store.BareDir(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	m := store.Meta{Fingerprint: stale, HostRoot: hr.Root, HostName: "orphan", Label: "o/orphan", Branch: "repo/" + stale, Mono: true}
	if err := store.SaveMeta(m); err != nil {
		t.Fatal(err)
	}

	f := classify(m, nil)
	if f == nil {
		t.Fatal("classify found nothing wrong with an orphaned overlay")
	}
	if f.kind != "fingerprint" {
		t.Errorf("kind = %q, want %q", f.kind, "fingerprint")
	}
	if !f.anomaly {
		t.Error("orphan should be an anomaly: re-keying moves a directory and wants the operator present")
	}
	if f.fixable {
		t.Error("doctor must not offer to re-key as part of a bulk --fix")
	}
	if !strings.Contains(f.detail, "attic rekey") || !strings.Contains(f.detail, hr.Fingerprint()) {
		t.Errorf("detail should name the repair and the live fingerprint, got %q", f.detail)
	}
}

// TestClassifyOverFetchFindsForeignRefs covers the state a narrowed refspec leaves behind: the fetch
// stops widening, and everything earlier fetches pulled stays, uncollectable by prune because those
// branches still exist on the remote and now sit outside the refspec.
func TestClassifyOverFetchFindsForeignRefs(t *testing.T) {
	fx := newOverFetchFixture(t)

	f := classifyOverFetch(fx.meta)
	if f == nil {
		t.Fatal("classifyOverFetch found nothing in an overlay holding three foreign refs")
	}
	if f.kind != findingOverFetch {
		t.Errorf("kind = %q, want %q", f.kind, findingOverFetch)
	}
	if !f.fixable {
		t.Error("over-fetch is reclaimable by --fix, so it must count as fixable")
	}
	// The wide clone leaves five refs that are not this overlay's: the labels branch and the other
	// project's branch in refs/heads/, both again under refs/remotes/, plus the origin/HEAD symref.
	want := []string{
		"refs/heads/_attic/labels",
		"refs/heads/repo/bbbbbbbbbbbb",
		"refs/remotes/origin/HEAD",
		"refs/remotes/origin/_attic/labels",
		"refs/remotes/origin/repo/bbbbbbbbbbbb",
	}
	if !slices.Equal(f.staleRefs, want) {
		t.Errorf("staleRefs = %v, want %v", f.staleRefs, want)
	}
	for _, r := range f.staleRefs {
		if r == overlayBranchRef(fx.fp) || r == "refs/remotes/origin/"+overlayBranch(fx.fp) {
			t.Errorf("staleRefs names the overlay's own ref %q", r)
		}
	}
	if !strings.Contains(f.detail, "--fix") {
		t.Errorf("detail should name the repair, got %q", f.detail)
	}
}

// A per-host overlay owns its whole bare, so every ref in it belongs and doctor must stay quiet.
func TestClassifyOverFetchIgnoresSoloOverlays(t *testing.T) {
	fx := newOverFetchFixture(t)
	solo := fx.meta
	solo.Mono = false
	if f := classifyOverFetch(solo); f != nil {
		t.Errorf("reported over-fetch on a per-host overlay: %+v", f)
	}
}

// An overlay holding only its own refs is the healthy shape, and must produce no finding — otherwise
// doctor exits non-zero forever and any hook gating on it is useless.
func TestClassifyOverFetchQuietWhenClean(t *testing.T) {
	fx := newOverFetchFixture(t)
	f := classifyOverFetch(fx.meta)
	if f == nil {
		t.Fatal("fixture should start over-fetched")
	}
	if err := reclaimOverlay(fx.fp, f.staleRefs); err != nil {
		t.Fatalf("reclaimOverlay: %v", err)
	}
	if again := classifyOverFetch(fx.meta); again != nil {
		t.Errorf("still reported after reclaim: %+v", again)
	}
}

// TestReclaimOverlayDropsForeignHistoryAndKeepsItsOwn is the one that matters: the reclaim must free
// the disk, and it must not touch a single object the overlay's own branch reaches.
func TestReclaimOverlayDropsForeignHistoryAndKeepsItsOwn(t *testing.T) {
	fx := newOverFetchFixture(t)
	repo := gitwrap.Repo{GitDir: fx.bare}

	ownTip, err := repo.Run("rev-parse", overlayBranchRef(fx.fp))
	if err != nil {
		t.Fatal(err)
	}
	ownFiles, err := repo.Run("ls-tree", "-r", "--name-only", overlayBranchRef(fx.fp))
	if err != nil {
		t.Fatal(err)
	}
	before := bareSizeKB(fx.bare)

	f := classifyOverFetch(fx.meta)
	if f == nil {
		t.Fatal("fixture should start over-fetched")
	}
	if err := reclaimOverlay(fx.fp, f.staleRefs); err != nil {
		t.Fatalf("reclaimOverlay: %v", err)
	}

	if got, err := repo.Run("rev-parse", overlayBranchRef(fx.fp)); err != nil {
		t.Fatalf("own branch gone: %v", err)
	} else if got != ownTip {
		t.Errorf("own tip moved: %q, want %q", got, ownTip)
	}
	if got, err := repo.Run("ls-tree", "-r", "--name-only", overlayBranchRef(fx.fp)); err != nil {
		t.Fatalf("own tree unreadable: %v", err)
	} else if got != ownFiles {
		t.Errorf("own file list changed:\n%s\nwant\n%s", got, ownFiles)
	}
	// Every blob the overlay's own commit reaches must still be readable, not merely referenced.
	for _, line := range splitLines(ownFiles) {
		if _, err := repo.Run("cat-file", "-e", overlayBranchRef(fx.fp)+":"+line); err != nil {
			t.Errorf("own blob %q unreadable after reclaim: %v", line, err)
		}
	}
	// The foreign commit must be gone, which is the whole point; a repack that keeps it reclaims nothing.
	if _, err := repo.Run("cat-file", "-e", fx.foreignTip+"^{commit}"); err == nil {
		t.Error("foreign commit survived the reclaim, so nothing was actually reclaimed")
	}
	if after := bareSizeKB(fx.bare); after >= before {
		t.Errorf("bare did not shrink: %dK -> %dK", before, after)
	}
	// fsck must be clean: deleting refs/remotes/origin/HEAD with update-ref leaves a dangling symref.
	if out, err := repo.Run("fsck", "--no-progress", "--no-dangling"); err != nil || strings.TrimSpace(out) != "" {
		t.Errorf("fsck after reclaim: err=%v out=%q", err, out)
	}
}

func TestHumanKB(t *testing.T) {
	cases := []struct {
		kb   int64
		want string
	}{
		{0, "0K"}, {200, "200K"}, {1023, "1023K"}, {1024, "1M"}, {48936, "47M"}, {1024 * 1024, "1.0G"},
	}
	for _, tc := range cases {
		if got := humanKB(tc.kb); got != tc.want {
			t.Errorf("humanKB(%d) = %q, want %q", tc.kb, got, tc.want)
		}
	}
}

type overFetchFixture struct {
	fp         string
	bare       string
	meta       store.Meta
	foreignTip string
}

// newOverFetchFixture builds a mono overlay cloned wide from a stand-in mono remote: foreign branches
// land in refs/heads/ as well as refs/remotes/, and origin/HEAD is a symref, which is the shape that
// made two earlier cleanup attempts silently reclaim nothing.
func newOverFetchFixture(t *testing.T) overFetchFixture {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	const fp = "aaaaaaaaaaaa"
	const foreign = "bbbbbbbbbbbb"
	remote := t.TempDir()
	git(t, remote, "init", "-q", "-b", labelsBranch, ".")
	writeFile(t, remote, "labels.toml", "[hosts]\n")
	git(t, remote, "add", "-A")
	git(t, remote, "commit", "-qm", "labels")
	for _, b := range []string{overlayBranch(fp), overlayBranch(foreign)} {
		git(t, remote, "checkout", "-q", "--orphan", b)
		git(t, remote, "rm", "-rq", "--cached", "--ignore-unmatch", ".")
		writeFile(t, remote, "docs-internal/a.md", filler(b))
		writeFile(t, remote, "docs-internal/b.md", filler(b+"-second"))
		git(t, remote, "add", "-A")
		git(t, remote, "commit", "-qm", "overlay "+b)
	}
	git(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+labelsBranch)
	git(t, remote, "checkout", "-qf", labelsBranch)

	bare, err := store.BareDir(fp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (gitwrap.Repo{}).Stream("clone", "--quiet", "--bare", remote, bare); err != nil {
		t.Fatalf("clone bare: %v", err)
	}
	repo := gitwrap.Repo{GitDir: bare}
	if err := repo.Stream("symbolic-ref", "HEAD", overlayBranchRef(fp)); err != nil {
		t.Fatal(err)
	}
	// The wildcard is what an overlay wired before the refspec was scoped still carries.
	if err := repo.Stream("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Stream("fetch", "--quiet", "origin"); err != nil {
		t.Fatal(err)
	}
	_ = repo.Stream("remote", "set-head", "origin", "-a")

	m := store.Meta{Fingerprint: fp, HostRoot: t.TempDir(), HostName: "over", Label: "o/over",
		Branch: overlayBranch(fp), Mono: true, Remote: remote}
	if err := store.SaveMeta(m); err != nil {
		t.Fatal(err)
	}
	tip, err := (gitwrap.Repo{}).Run("-C", remote, "rev-parse", "refs/heads/"+overlayBranch(foreign))
	if err != nil {
		t.Fatal(err)
	}
	return overFetchFixture{fp: fp, bare: bare, meta: m, foreignTip: strings.TrimSpace(tip)}
}

// filler produces compressible bytes so the fixture's packs are big enough for a size change to
// register, without random data that a secret scanner would flag.
func filler(seed string) string {
	var b strings.Builder
	for i := range 20000 {
		fmt.Fprintf(&b, "%s line %d\n", seed, i)
	}
	return b.String()
}

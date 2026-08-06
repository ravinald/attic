package cmd

import (
	"os"
	"strings"
	"testing"

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

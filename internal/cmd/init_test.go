package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/ravinald/attic/internal/host"
	"github.com/ravinald/attic/internal/ignore"
)

// A second machine running init against a remote that already carries repo/<fp> starts an unrelated
// history, and finds out only when its first push is rejected. init must refuse and name clone, leave
// no bare behind, and still allow the documented two-machine merge through --unrelated.
func TestInitRefusesExistingMonoBranch(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{"docs-internal/a.md": "doc"})
	setMonoDefault(t, "")
	prev := initFlags
	t.Cleanup(func() { initFlags = prev })
	initFlags.monoRemote = fx.remote

	err := initCmd.RunE(initCmd, nil)
	if err == nil {
		t.Fatal("init started a second history on an existing repo/<fp>")
	}
	if !strings.Contains(err.Error(), "attic clone") || !strings.Contains(err.Error(), "--unrelated") {
		t.Errorf("refusal should name clone and --unrelated: %v", err)
	}
	if _, statErr := os.Stat(fx.bare); !os.IsNotExist(statErr) {
		t.Errorf("refused init left a bare behind at %s", fx.bare)
	}

	initFlags.unrelated = true
	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatalf("init --unrelated: %v", err)
	}
	if _, err := os.Stat(fx.bare); err != nil {
		t.Errorf("init --unrelated provisioned no bare: %v", err)
	}
}

// A bad flag combination must not leave a bare that turns every retry into "overlay already exists".
func TestInitRollsBackOnBadFlags(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{"docs-internal/a.md": "doc"})
	prev := initFlags
	t.Cleanup(func() { initFlags = prev })
	initFlags.remote = fx.remote
	initFlags.monoRemote = fx.remote

	if err := initCmd.RunE(initCmd, nil); err == nil {
		t.Fatal("init accepted --remote with --mono-remote")
	}
	if _, err := os.Stat(fx.bare); !os.IsNotExist(err) {
		t.Errorf("failed init left a bare behind at %s", fx.bare)
	}
}

// A committed attic block on a checkout with no overlay means another machine has one, and pointing
// the user at init there starts an empty history beside the real one.
func TestMissingOverlayErrorPointsAtCloneWhenBlockExists(t *testing.T) {
	fx := newCloneFixture(t, map[string]string{"docs-internal/a.md": "doc"})
	setMonoDefault(t, "")
	hr, err := host.Detect(fx.hostRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got := noOverlayError(hr).Error(); !strings.Contains(got, "attic init") {
		t.Errorf("with no block, the error should still offer init: %q", got)
	}

	blk, err := ignore.Load(gitignorePath(hr))
	if err != nil {
		t.Fatal(err)
	}
	blk.Add("docs-internal")
	if err := blk.Save(); err != nil {
		t.Fatal(err)
	}
	got := noOverlayError(hr).Error()
	if strings.Contains(got, "attic init") || !strings.Contains(got, "attic clone") {
		t.Errorf("with a block, the error should name clone and not init: %q", got)
	}
}

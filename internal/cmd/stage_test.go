package cmd

import (
	"strings"
	"testing"

	"github.com/ravinald/attic/internal/ignore"
)

// TestRejectUnregistered guards the seam between the two verbs. Staging a path the block does not
// cover would put it in the overlay index while the host still ignores nothing on its behalf, so the
// next `git add -A` carries it upstream — the leak attic exists to prevent.
func TestRejectUnregistered(t *testing.T) {
	blk := ignore.Block{Lines: []string{"docs-internal"}}

	if err := rejectUnregistered(blk, []string{"docs-internal", "docs-internal/new.md"}); err != nil {
		t.Errorf("registered and covered paths rejected: %v", err)
	}
	err := rejectUnregistered(blk, []string{"secrets"})
	if err == nil {
		t.Fatal("staging an unregistered path was allowed")
	}
	if !strings.Contains(err.Error(), "attic add secrets") {
		t.Errorf("error should name the registering command, got %q", err)
	}
}

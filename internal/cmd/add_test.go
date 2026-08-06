package cmd

import (
	"reflect"
	"slices"
	"testing"

	"github.com/ravinald/attic/internal/ignore"
	"github.com/ravinald/attic/internal/store"
)

// TestPartitionRegistered pins the distinction `attic add` diverges from `git add` to make: a path the
// block already covers must not produce another rule, because the extra line ignores nothing further
// and misreports the granularity the overlay is managed at.
func TestPartitionRegistered(t *testing.T) {
	blk := ignore.Block{Lines: []string{"docs-internal", ".drover.toml", "logs/*.json"}}

	fresh, already := partitionRegistered(blk, []string{
		"docs-internal",                         // exact entry
		"docs-internal/CHANGELOG_2026_08_06.md", // covered by the directory
		"notes",                                 // genuinely new
		"logs/a.json",                           // a glob entry never counts as covering
	})

	if want := []string{"notes", "logs/a.json"}; !slices.Equal(fresh, want) {
		t.Errorf("fresh = %q, want %q", fresh, want)
	}
	if len(already) != 2 {
		t.Fatalf("already = %+v, want 2 entries", already)
	}
	if already[0].by != "docs-internal" || already[0].path != "docs-internal" {
		t.Errorf("exact entry misreported: %+v", already[0])
	}
	if already[1].by != "docs-internal" {
		t.Errorf("covered path should name the directory entry, got %+v", already[1])
	}
}

func TestDuplicateScope(t *testing.T) {
	settled := ignore.Block{Lines: []string{"docs-internal"}}

	cases := []struct {
		name string
		mode string
		blk  ignore.Block
		rels []string
		want []string
	}{
		{"off scans nothing", store.OnDuplicateOff, ignore.Block{}, []string{"docs-internal"}, nil},
		{"warn on first add", store.OnDuplicateWarn, ignore.Block{}, []string{"docs-internal"}, []string{"docs-internal"}},
		{"warn stays quiet on re-add", store.OnDuplicateWarn, settled, []string{"docs-internal"}, nil},
		{"warn reports only the new path", store.OnDuplicateWarn, settled, []string{"docs-internal", "CLAUDE.md"}, []string{"CLAUDE.md"}},
		{"manage still absorbs on re-add", store.OnDuplicateManage, settled, []string{"docs-internal"}, []string{"docs-internal"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := duplicateScope(tc.mode, tc.blk, tc.rels); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("duplicateScope(%q, %q) = %q, want %q", tc.mode, tc.rels, got, tc.want)
			}
		})
	}
}

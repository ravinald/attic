package cmd

import (
	"reflect"
	"testing"

	"github.com/ravinald/attic/internal/ignore"
	"github.com/ravinald/attic/internal/store"
)

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

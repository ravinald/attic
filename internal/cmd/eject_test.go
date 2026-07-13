package cmd

import (
	"reflect"
	"testing"
)

func TestTopLevels(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nested files collapse to dir", []string{"docs-internal/a.md", "docs-internal/b/c.md"}, []string{"docs-internal"}},
		{"mixed roots sorted+deduped", []string{"z/1", "a/2", "a/3", "z/4"}, []string{"a", "z"}},
		{"bare file keeps its name", []string{"NOTES.md"}, []string{"NOTES.md"}},
		{"empties skipped", []string{"", "docs/x", ""}, []string{"docs"}},
		{"none", nil, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := topLevels(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("topLevels(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

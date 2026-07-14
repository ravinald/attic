package cmd

import (
	"testing"

	"github.com/ravinald/attic/internal/store"
)

func TestResolveLabelWith(t *testing.T) {
	auto := store.Meta{Fingerprint: "abc", Label: "owner/repo", HostName: "repo"}
	bare := store.Meta{Fingerprint: "xyz", HostName: "repo"}

	cases := []struct {
		name      string
		meta      store.Meta
		overrides map[string]string
		want      string
	}{
		{"override wins over auto", auto, map[string]string{"abc": "mine"}, "mine"},
		{"auto when no override", auto, nil, "owner/repo"},
		{"basename when no label", bare, nil, "repo"},
		{"override for one, basename for other", bare, map[string]string{"abc": "mine"}, "repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveLabelWith(tc.meta, tc.overrides); got != tc.want {
				t.Fatalf("resolveLabelWith = %q, want %q", got, tc.want)
			}
		})
	}
}

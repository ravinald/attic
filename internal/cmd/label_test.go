package cmd

import (
	"fmt"
	"strings"
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

// TestSoleMonoRemote is the regression for a machine that could not infer its own mono remote. Nine
// overlays there synced to one repo through three spellings of its URL, and dedupe on the raw string
// reported three remotes and refused. The counts below are that machine's, 7/1/1.
func TestSoleMonoRemote(t *testing.T) {
	const (
		slash = "https://github.com/ravinald/attic-overlays/"
		bare  = "https://github.com/ravinald/attic-overlays"
		dot   = "https://github.com/ravinald/attic-overlays.git"
		ssh   = "git@github.com:ravinald/attic-overlays.git"
		other = "https://github.com/ravinald/other-overlays"
	)
	cases := []struct {
		name    string
		remotes []string
		solo    []string // recorded with mono = false, so they must be ignored entirely
		want    string
		wantErr string
	}{
		{name: "no mono overlays", wantErr: "no mono-remote overlays"},
		{name: "one spelling", remotes: []string{slash, slash}, want: slash},
		{
			name:    "three spellings of one remote",
			remotes: []string{slash, slash, slash, slash, slash, slash, slash, bare, dot},
			want:    slash,
		},
		{
			name:    "tie breaks lexicographically so runs agree",
			remotes: []string{bare, dot},
			want:    bare,
		},
		{
			name:    "ssh and https stay two remotes",
			remotes: []string{slash, ssh},
			wantErr: "more than one mono remote",
		},
		{
			name:    "genuinely different repos stay two remotes",
			remotes: []string{slash, other},
			wantErr: "more than one mono remote",
		},
		{
			name:    "a solo overlay is not a mono remote",
			solo:    []string{other},
			remotes: []string{slash},
			want:    slash,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			seed := func(remote string, mono bool, i int) {
				fp := fmt.Sprintf("%012d", i)
				if err := store.SaveMeta(store.Meta{Fingerprint: fp, Remote: remote, Mono: mono}); err != nil {
					t.Fatal(err)
				}
			}
			n := 0
			for _, r := range tc.remotes {
				seed(r, true, n)
				n++
			}
			for _, r := range tc.solo {
				seed(r, false, n)
				n++
			}

			got, err := soleMonoRemote()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("soleMonoRemote() = %q, want error %q", got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("soleMonoRemote: %v", err)
			}
			if got != tc.want {
				t.Errorf("soleMonoRemote() = %q, want %q", got, tc.want)
			}
		})
	}
}

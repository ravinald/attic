package host

import "testing"

func TestParseOwnerRepo(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{"scp", "git@github.com:acme/widgets.git", "acme/widgets", true},
		{"scp no .git", "git@github.com:acme/widgets", "acme/widgets", true},
		{"https", "https://github.com/acme/widgets.git", "acme/widgets", true},
		{"https no .git", "https://github.com/acme/widgets", "acme/widgets", true},
		{"https trailing slash", "https://github.com/acme/widgets/", "acme/widgets", true},
		{"ssh url", "ssh://git@github.com/acme/widgets.git", "acme/widgets", true},
		{"gitlab subgroup", "git@gitlab.com:group/sub/proj.git", "group/sub/proj", true},
		{"empty", "", "", false},
		{"host only", "git@github.com:", "", false},
		{"no owner", "https://github.com/widgets.git", "", false},
		{"garbage", "not a url", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseOwnerRepo(tc.url)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ParseOwnerRepo(%q) = (%q, %v), want (%q, %v)", tc.url, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestWebBase(t *testing.T) {
	cases := []struct {
		url  string
		want string
		ok   bool
	}{
		{"git@github.com:acme/widgets.git", "https://github.com/acme/widgets", true},
		{"https://github.com/acme/widgets.git", "https://github.com/acme/widgets", true},
		{"ssh://git@gitlab.com/group/sub/proj.git", "https://gitlab.com/group/sub/proj", true},
		{"", "", false},
		{"not a url", "", false},
	}
	for _, tc := range cases {
		got, ok := WebBase(tc.url)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("WebBase(%q) = (%q, %v), want (%q, %v)", tc.url, got, ok, tc.want, tc.ok)
		}
	}
}

// TestCanonicalRemote pins what counts as one remote. The three https spellings below were all found
// on one machine, recorded by the same tool against the same repo, and read as three remotes.
func TestCanonicalRemote(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"https plain", "https://github.com/ravinald/attic-overlays", "https://github.com/ravinald/attic-overlays", true},
		{"https trailing slash", "https://github.com/ravinald/attic-overlays/", "https://github.com/ravinald/attic-overlays", true},
		{"https dot git", "https://github.com/ravinald/attic-overlays.git", "https://github.com/ravinald/attic-overlays", true},
		{"host case is irrelevant", "https://GitHub.com/ravinald/attic-overlays", "https://github.com/ravinald/attic-overlays", true},
		{"scp style is ssh", "git@github.com:ravinald/attic-overlays.git", "ssh://github.com/ravinald/attic-overlays", true},
		{"ssh url matches scp style", "ssh://git@github.com/ravinald/attic-overlays", "ssh://github.com/ravinald/attic-overlays", true},
		{"gitlab subgroup keeps its path", "https://gitlab.com/group/sub/repo.git", "https://gitlab.com/group/sub/repo", true},
		{"empty", "", "", false},
		{"no path", "https://github.com", "", false},
		{"local path is not a remote key", "/srv/mirrors/overlays", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CanonicalRemote(tc.raw)
			if ok != tc.ok {
				t.Fatalf("CanonicalRemote(%q) ok = %v, want %v", tc.raw, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("CanonicalRemote(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// ssh and https to one repo are the same project and not the same remote: only one of them may have
// credentials on a given machine, so collapsing them would hand back a URL that cannot authenticate.
func TestCanonicalRemoteKeepsTransportsApart(t *testing.T) {
	https, _ := CanonicalRemote("https://github.com/ravinald/attic-overlays/")
	ssh, _ := CanonicalRemote("git@github.com:ravinald/attic-overlays.git")
	if https == ssh {
		t.Errorf("https and ssh collapsed to one key: %q", https)
	}
}

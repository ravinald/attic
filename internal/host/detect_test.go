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

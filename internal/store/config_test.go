package store

import "testing"

func TestResolveOnDuplicatePrecedence(t *testing.T) {
	global := Config{Gitignore: GitignoreConfig{OnDuplicate: OnDuplicateOff}}

	cases := []struct {
		name            string
		flag, env, repo string
		want            string
	}{
		{"default when all unset", "", "", "", OnDuplicateWarn},
		{"global lowest", "", "", "", OnDuplicateOff}, // via global below
		{"repo beats global", "", "", OnDuplicateWarn, OnDuplicateWarn},
		{"env beats repo", "", OnDuplicateManage, OnDuplicateWarn, OnDuplicateManage},
		{"flag beats env", OnDuplicateWarn, OnDuplicateManage, OnDuplicateOff, OnDuplicateWarn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := Config{}
			if c.name == "global lowest" {
				g = global
			}
			got, err := ResolveOnDuplicate(c.flag, c.env, c.repo, g)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveOnDuplicateRejectsInvalid(t *testing.T) {
	if _, err := ResolveOnDuplicate("bogus", "", "", Config{}); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveConfig(Config{Gitignore: GitignoreConfig{OnDuplicate: OnDuplicateManage}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Gitignore.OnDuplicate != OnDuplicateManage {
		t.Fatalf("round-trip lost value: %+v", c)
	}
}

func TestLoadConfigMissingIsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Gitignore.OnDuplicate != "" {
		t.Fatalf("expected empty config, got %+v", c)
	}
}

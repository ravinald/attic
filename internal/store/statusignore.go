package store

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// SplitStatusIgnore parses a comma-separated pattern list, dropping empty and whitespace-only
// fields so a trailing comma or `set status.ignore ""` both mean "no patterns".
func SplitStatusIgnore(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// ValidStatusIgnorePattern reports whether p is a pattern StatusIgnored can evaluate. Callers
// validate on write so a typo fails at `attic config set` rather than silently matching nothing
// for months — a pattern that never fires looks identical to one that has nothing to match.
func ValidStatusIgnorePattern(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("empty pattern")
	}
	if _, err := path.Match(statusIgnoreProbe(p), "probe"); err != nil {
		return fmt.Errorf("bad pattern %q: %w", p, err)
	}
	return nil
}

// statusIgnoreProbe reduces a pattern to the part path.Match parses, dropping the "**/" and
// trailing-slash affixes StatusIgnored handles itself.
func statusIgnoreProbe(p string) string {
	return strings.TrimSuffix(strings.TrimPrefix(p, "**/"), "/")
}

// StatusIgnored reports whether the host-relative overlay path rel matches pattern.
//
// The rules are the gitignore subset people actually type, and deliberately not all of it:
// a pattern with no separator matches the basename at any depth (".DS_Store"), a trailing slash
// matches everything beneath a directory ("scratch/", at any depth when the name has no
// separator), and anything else matches the whole host-relative path ("docs-internal/*.tmp").
// A leading "**/" is accepted as a synonym for the basename form because that is what people
// reach for first, and a silent no-op there would be indistinguishable from a working filter.
func StatusIgnored(rel, pattern string) (bool, error) {
	p := strings.TrimPrefix(pattern, "**/")

	if dir, isDir := strings.CutSuffix(p, "/"); isDir {
		if strings.Contains(dir, "/") {
			return strings.HasPrefix(rel, dir+"/"), nil
		}
		for _, seg := range strings.Split(path.Dir(rel), "/") {
			if ok, err := path.Match(dir, seg); ok || err != nil {
				return ok, err
			}
		}
		return false, nil
	}

	if strings.Contains(p, "/") {
		return path.Match(p, rel)
	}
	return path.Match(p, path.Base(rel))
}

// ResolveStatusIgnore unions every layer rather than letting the highest one win. A list config
// that replaced instead of accumulating would silently drop the global ".DS_Store" the moment a
// repo set one pattern of its own — the opposite of what setting a per-repo pattern asks for.
// Order is env, per-repo, global; duplicates collapse to their first appearance.
func ResolveStatusIgnore(env, perRepo []string, global Config) []string {
	seen := map[string]bool{}
	var out []string
	for _, layer := range [][]string{env, perRepo, global.Status.Ignore} {
		for _, p := range layer {
			if p = strings.TrimSpace(p); p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// FilterStatusIgnored drops every path matching a pattern, returning the survivors plus any
// pattern too malformed to evaluate. Malformed patterns are reported rather than fatal: a typo in
// a hand-edited config must not take `attic status` down, but staying quiet about it would hide
// overlay files the user believes are being listed.
func FilterStatusIgnored(paths, patterns []string) (kept, malformed []string) {
	if len(patterns) == 0 {
		return paths, nil
	}
	bad := map[string]bool{}
	for _, rel := range paths {
		hidden := false
		for _, pat := range patterns {
			ok, err := StatusIgnored(rel, pat)
			if err != nil {
				if !bad[pat] {
					bad[pat] = true
					malformed = append(malformed, pat)
				}
				continue
			}
			if ok {
				hidden = true
				break
			}
		}
		if !hidden {
			kept = append(kept, rel)
		}
	}
	return kept, malformed
}

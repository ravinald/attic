// Package ignore manages a marker-delimited block inside a host repo's .gitignore so overlay paths cannot accidentally enter the upstream history.
package ignore

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
)

// BeginMarker / EndMarker delimit attic's section. Match by prefix so the trailing prose can change without breaking discovery.
const (
	BeginPrefix = "# BEGIN attic"
	EndPrefix   = "# END attic"
	beginLine   = "# BEGIN attic — managed by `attic`, do not edit between markers"
	endLine     = "# END attic"
)

// Block is the attic-managed section of a .gitignore file.
type Block struct {
	Path  string              // absolute path to the .gitignore file
	Lines []string            // entries inside the block, no trailing newlines
	drop  map[string]struct{} // outside-block lines to delete on Save (manage mode)
}

// Duplicate is a rule outside attic's block that already ignores a managed path.
type Duplicate struct {
	Line int    // 1-based line number in the .gitignore
	Text string // the rule as written, e.g. "/docs-internal/"
	Path string // the managed path it duplicates
}

// Load reads the block from path. A missing file or missing markers yields an empty block, which is a valid starting state.
func Load(path string) (Block, error) {
	b := Block{Path: path}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return b, fmt.Errorf("ignore: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	in := false
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, BeginPrefix):
			in = true
		case strings.HasPrefix(line, EndPrefix):
			in = false
		case in:
			if t := strings.TrimSpace(line); t != "" {
				b.Lines = append(b.Lines, t)
			}
		}
	}
	if err := s.Err(); err != nil {
		return b, fmt.Errorf("ignore: scan %s: %w", path, err)
	}
	return b, nil
}

// Add inserts paths into the block, deduplicating and keeping it sorted.
func (b *Block) Add(paths ...string) {
	set := make(map[string]struct{}, len(b.Lines))
	for _, l := range b.Lines {
		set[l] = struct{}{}
	}
	for _, p := range paths {
		if _, ok := set[p]; !ok {
			set[p] = struct{}{}
			b.Lines = append(b.Lines, p)
		}
	}
	sort.Strings(b.Lines)
}

// Remove drops paths from the block.
func (b *Block) Remove(paths ...string) {
	drop := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		drop[p] = struct{}{}
	}
	out := b.Lines[:0]
	for _, l := range b.Lines {
		if _, gone := drop[l]; !gone {
			out = append(out, l)
		}
	}
	b.Lines = out
}

// Has returns true if the block contains path.
func (b *Block) Has(path string) bool {
	for _, l := range b.Lines {
		if l == path {
			return true
		}
	}
	return false
}

// Covers returns the block line that already ignores path by being a directory ancestor of it. An
// exact match is Has, not Covers, and a line carrying glob metacharacters never matches — attic will
// not second-guess a real pattern's intent, matching FindDuplicates' rule.
//
// This is the check Add cannot make: Add dedupes by exact string, so without it registering a file
// beneath an already-registered directory appends a line that ignores nothing new and misreports the
// granularity the overlay is managed at.
func (b *Block) Covers(path string) (string, bool) {
	p := normalizeRule(path)
	if p == "" {
		return "", false
	}
	for _, l := range b.Lines {
		n := normalizeRule(l)
		if n == "" || n == p || hasGlobMeta(n) {
			continue
		}
		if strings.HasPrefix(p, n+"/") {
			return l, true
		}
	}
	return "", false
}

// DropOutside marks non-block lines (matched by exact trimmed text) for deletion on the next Save.
// The manage-mode policy uses it to remove a redundant rule once its path lives in the block. Lines
// inside the markers are never affected — those are owned by Lines.
func (b *Block) DropOutside(lines ...string) {
	if b.drop == nil {
		b.drop = make(map[string]struct{}, len(lines))
	}
	for _, l := range lines {
		b.drop[strings.TrimSpace(l)] = struct{}{}
	}
}

// FindDuplicates reports rules outside the attic block, in gitignorePath, that redundantly ignore any
// of paths. Matching is slash-insensitive (/foo/ ≡ foo); lines carrying glob metacharacters never
// match, because attic won't second-guess a real pattern's intent. A missing file yields no matches.
func FindDuplicates(gitignorePath string, paths []string) ([]Duplicate, error) {
	f, err := os.Open(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ignore: open %s: %w", gitignorePath, err)
	}
	defer func() { _ = f.Close() }()

	want := make(map[string]string, len(paths)) // normalised key -> original path
	for _, p := range paths {
		want[normalizeRule(p)] = p
	}

	var dups []Duplicate
	s := bufio.NewScanner(f)
	in := false
	for n := 1; s.Scan(); n++ {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, BeginPrefix):
			in = true
			continue
		case strings.HasPrefix(line, EndPrefix):
			in = false
			continue
		}
		if in {
			continue
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || hasGlobMeta(t) {
			continue
		}
		if p, ok := want[normalizeRule(t)]; ok {
			dups = append(dups, Duplicate{Line: n, Text: t, Path: p})
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("ignore: scan %s: %w", gitignorePath, err)
	}
	return dups, nil
}

// normalizeRule collapses slash-only differences so /docs-internal/ and docs-internal compare equal.
func normalizeRule(s string) string { return strings.Trim(strings.TrimSpace(s), "/") }

// hasGlobMeta reports whether a rule carries gitignore pattern syntax that makes exact-match unsafe.
func hasGlobMeta(s string) bool { return strings.ContainsAny(s, "*?[]!") }

// Save splices the block back into the file atomically, preserving content outside the markers. If the markers don't exist, the block is appended.
func (b *Block) Save() error {
	existing, err := os.ReadFile(b.Path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ignore: read %s: %w", b.Path, err)
	}
	var out bytes.Buffer
	if len(existing) == 0 {
		if len(b.Lines) == 0 {
			return nil
		}
		writeBlock(&out, b.Lines)
	} else {
		s := bufio.NewScanner(bytes.NewReader(existing))
		spliced, skipping := false, false
		for s.Scan() {
			line := s.Text()
			switch {
			case strings.HasPrefix(line, BeginPrefix):
				skipping = true
				if len(b.Lines) > 0 {
					writeBlock(&out, b.Lines)
				}
				spliced = true
			case strings.HasPrefix(line, EndPrefix):
				skipping = false
			case !skipping:
				if _, gone := b.drop[strings.TrimSpace(line)]; gone {
					continue
				}
				out.WriteString(line)
				out.WriteByte('\n')
			}
		}
		if err := s.Err(); err != nil {
			return fmt.Errorf("ignore: scan %s: %w", b.Path, err)
		}
		if !spliced && len(b.Lines) > 0 {
			if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
				out.WriteByte('\n')
			}
			writeBlock(&out, b.Lines)
		}
	}
	tmp := b.Path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o644); err != nil {
		return fmt.Errorf("ignore: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, b.Path); err != nil {
		return fmt.Errorf("ignore: rename %s: %w", tmp, err)
	}
	return nil
}

func writeBlock(w *bytes.Buffer, lines []string) {
	w.WriteString(beginLine)
	w.WriteByte('\n')
	for _, l := range lines {
		w.WriteString(l)
		w.WriteByte('\n')
	}
	w.WriteString(endLine)
	w.WriteByte('\n')
}

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
	Path  string   // absolute path to the .gitignore file
	Lines []string // entries inside the block, no trailing newlines
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

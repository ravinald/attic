// Package gitwrap shells out to git with explicit --git-dir and --work-tree, the trick that lets attic store overlay history outside the host work tree.
package gitwrap

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Repo represents a bare overlay git repo bound to a host work tree.
type Repo struct {
	GitDir   string
	WorkTree string
}

// Run executes git and captures stdout. Stderr passes through to the parent so users see git's own diagnostics.
func (r Repo) Run(args ...string) (string, error) {
	c := r.cmd(args...)
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return string(out), wrap(err, args)
	}
	return string(out), nil
}

// Stream executes git with stdin/stdout/stderr connected to the parent. Use for interactive subcommands (push, pull) and pager-friendly ones (log, diff).
func (r Repo) Stream(args ...string) error {
	c := r.cmd(args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return wrap(err, args)
	}
	return nil
}

// StreamTo executes git with stdout sent to w.
func (r Repo) StreamTo(w io.Writer, args ...string) error {
	c := r.cmd(args...)
	c.Stdout = w
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return wrap(err, args)
	}
	return nil
}

// Succeeded reports whether git exited zero. It is for the --quiet/--exit-code predicates, where a
// non-zero exit is an answer rather than a failure; the error return stays reserved for git not
// running at all. Output is discarded — ask a predicate a question, don't print its reasoning.
func (r Repo) Succeeded(args ...string) (bool, error) {
	c := r.cmd(args...)
	if err := c.Run(); err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return false, nil
		}
		return false, wrap(err, args)
	}
	return true, nil
}

func (r Repo) cmd(args ...string) *exec.Cmd {
	var prefix []string
	if r.GitDir != "" {
		prefix = append(prefix, "--git-dir="+r.GitDir)
	}
	if r.WorkTree != "" {
		prefix = append(prefix, "--work-tree="+r.WorkTree)
	}
	return exec.Command("git", append(prefix, args...)...)
}

func wrap(err error, args []string) error {
	return fmt.Errorf("gitwrap: git %s failed: %w", strings.Join(args, " "), err)
}

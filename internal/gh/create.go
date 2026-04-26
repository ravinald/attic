// Package gh provides best-effort integration with the GitHub CLI for one-shot remote creation.
package gh

import (
	"fmt"
	"os/exec"
	"strings"
)

// Available reports whether the gh CLI is installed and on PATH.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// CreatePrivate creates an empty private repository on GitHub via the gh CLI and returns its SSH URL.
// name should be in "owner/repo" form, or just "repo" to use the authenticated user.
func CreatePrivate(name, description string) (string, error) {
	if !Available() {
		return "", fmt.Errorf("gh: gh CLI not found in PATH — install from https://cli.github.com or set --remote manually")
	}
	args := []string{"repo", "create", name, "--private"}
	if description != "" {
		args = append(args, "--description", description)
	}
	out, err := exec.Command("gh", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh: repo create %s: %w (output: %s)", name, err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://github.com/") {
			return httpsToSSH(line), nil
		}
	}
	return "", fmt.Errorf("gh: could not parse repo URL from output: %s", strings.TrimSpace(string(out)))
}

func httpsToSSH(httpsURL string) string {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(httpsURL, prefix) {
		return httpsURL
	}
	return "git@github.com:" + strings.TrimSuffix(strings.TrimPrefix(httpsURL, prefix), "/") + ".git"
}

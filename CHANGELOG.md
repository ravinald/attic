# Changelog

All notable changes to `attic`.

## [v0.1.0] — unreleased

Initial public release.

### Features

- `attic init [--remote URL | --mono-remote URL | --gh-private | --no-flag]` — create a per-host bare overlay under `$XDG_DATA_HOME/attic/repos/<fp>/`. Host identity is the host repo's root commit SHA (12-char prefix). Mutually-exclusive remote modes:
  - per-host (`--remote`): one git repo per host, branch `main`
  - mono (`--mono-remote`): one shared git repo, branch `host/<fp>` per host. `push.default=current` + `push.autoSetupRemote=true` configured automatically.
  - `--gh-private`: per-host mode with `gh repo create --private` integration.
- `attic clone <remote> [--mono]` — restore an overlay on a fresh machine. Mono variant single-branch-clones `host/<fp>` only and pre-flights with `git ls-remote` for a useful error if the branch is missing. Refuses to clobber existing files unless `--force`.
- `attic add <path>...` / `attic rm <path>... [--delete]` — manage paths in the marker-delimited `# BEGIN attic` / `# END attic` block of the host repo's `.gitignore`. Atomic write via tmp + rename. `add` uses `git add --force` (the force is intentional — gitignored upstream and tracked in the overlay is the design).
- `attic commit`, `status`, `push`, `pull`, `fetch`, `log`, `diff`, `ls` — pass-throughs to git in the overlay context.
- `attic where [--fp]` — print bare path, fingerprint, branch, remote.
- `attic exec -- <git-args>` — escape hatch.
- `attic version` — version, commit, build date (baked via `-ldflags`).

### Quality

- `golangci-lint` (v2 schema) clean.
- `govulncheck` clean.
- Unit tests for the `ignore.Block` splice/idempotence logic.

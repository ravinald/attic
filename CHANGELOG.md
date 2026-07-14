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
- Labels now auto-derive from the host repo's origin as `owner/repo` at `init`/`clone` — no manual `attic label set` for the common case. Provenance is tracked (`label_source = origin|manual`) so a hand-set label is never silently overwritten. `attic label set` still accepts any name, and a label may now contain `/` (an `owner/repo` slug); leading/trailing `/`, `//`, and `..` segments stay rejected.
- `attic doctor [--fix] [--force]` — sweeps every overlay on the machine and reports labels that drifted from their origin slug (or origins that moved). Reports by default and exits non-zero on fixable drift (hook-friendly); `--fix` rewrites auto-derived labels and refreshes moved origins; `--force` also adopts the origin slug over a hand-set label. Touches only local meta — run `attic labels push` to publish.
- `attic labels push` now also writes a `README.md` on the `_attic/labels` branch: a markdown table linking each `owner/repo` label to its `host/<fp>` branch, so the mono remote is browsable on the web without decoding `labels.toml`.

### Quality

- `golangci-lint` (v2 schema) clean.
- `govulncheck` clean.
- Unit tests for the `ignore.Block` splice/idempotence logic.

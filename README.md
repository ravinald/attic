# attic

Track files alongside a git repo without committing them to it.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
Status: **alpha** (v0.1)

`attic` keeps a per-host-repo *bare* git overlay outside the host work tree. Files like `docs-internal/`, a project-local `CLAUDE.md`, scratch notes, or a per-machine `.envrc` live in the host work tree where you actually use them — but their history lives elsewhere, on its own remote, and a marker block in the host's `.gitignore` stops them ever leaking upstream.

## How it works

The trick is `vcsh`'s, just per-project: `git --git-dir=<bare-outside-the-repo> --work-tree=<host-repo-root>`. Every `attic` subcommand is a thin wrapper that fills in those flags.

State lives at:

```
~/.local/share/attic/
  repos/<root-commit-sha-12>/
    attic.git/   # the bare overlay
    meta.toml   # host_root, remote, name, created_at
```

Identity is the host repo's root commit SHA — stable across machines and clones. So on a new machine: clone the host repo, `cd` in, `attic clone <remote>`, and your overlay files reappear under the same fingerprint.

## Install

```sh
go install github.com/ravinald/attic/cmd/attic@latest
```

Or from a checkout:

```sh
make install
```

## Quickstart

Two ways to host overlays remotely. Pick one and stay with it.

### Mono remote (recommended) — one shared repo, branch per host

```sh
# One-time setup: a single private repo holds every overlay you'll ever have.
gh repo create ravinald/attic-overlays --private

# In each host repo:
cd ~/git/wifimgr
attic init --mono-remote git@github.com:ravinald/attic-overlays.git
attic add docs-internal/
attic commit -m "wifimgr notes"
attic push                       # → branch host/<fingerprint>
```

On another machine, after cloning the host repo:

```sh
cd ~/git/wifimgr
attic clone --mono git@github.com:ravinald/attic-overlays.git
ls docs-internal/                # back
```

The fingerprint (host repo's root commit SHA) is the branch name on the shared remote, so the same `attic clone --mono <url>` works across all your projects without remembering names.

### Per-host remote — one private GitHub repo per project

```sh
cd ~/git/myrepo
attic init --gh-private          # creates ravinald/myrepo-attic via gh
attic add docs-internal/
attic commit -m "first overlay"
attic push
```

On another machine:

```sh
cd ~/git/myrepo
attic clone git@github.com:ravinald/myrepo-attic.git
```

## Commands

| Command | Purpose |
|---|---|
| `attic init [--remote URL \| --mono-remote URL \| --gh-private]` | Create overlay for the current host repo. |
| `attic deinit [--force]` | Remove the local overlay + `.gitignore` block (work-tree files stay). Refuses to drop unpushed commits without `--force`. |
| `attic clone <remote> [--mono]` | Restore an existing overlay on a new machine. |
| `attic add <path>...` | Stage paths and append to host `.gitignore` block. |
| `attic rm <path>... [--delete]` | Stop tracking; `--delete` also removes the file. |
| `attic eject [--check]` | Evict managed paths from the **host** index (never from disk or the overlay); `--check` reports without changing. |
| `attic commit [-m <msg>]` | Commit staged overlay changes. Without `-m`, uses a timestamped snapshot message. |
| `attic status` `push` `pull` `fetch` `log` `diff` | Pass-through to git. |
| `attic sync [--strategy=rebase\|merge]` | Fetch + integrate + push. Refuses on dirty work tree. |
| `attic ls` | List paths tracked in the overlay. |
| `attic list [--fetch] [--wide] [--json]` | Show every overlay on this machine with label, fp, sync state. |
| `attic where [--fp]` | Print bare path, fingerprint, remote. |
| `attic label get` / `attic label set <name>` | Read or set the current overlay's label (auto-set to `owner/repo` at init). |
| `attic labels push` / `attic labels pull` | Sync the host-id → label mapping across machines via the mono remote. |
| `attic doctor [--fix] [--force] [--push]` | Audit every overlay's label against its origin remote; report drift, `--fix` corrects it, `--push` publishes the corrected map. |
| `attic exec -- <git-args>` | Run any git command against the overlay. |
| `attic version` | Version, commit, build date. |

## Labels: naming your overlays

An overlay's identity is the host repo's root-commit SHA — stable, but unreadable. On the mono remote every project is a branch named `host/<fingerprint>`:

```
host/8b88ecad3aa9
host/3f2a9c1d5e7b
host/a1b2c3d4e5f6
```

Nothing there tells you which branch is `wifimgr` and which is `attic`. A **label** is a fingerprint → human-name mapping that makes the listing legible without changing where anything is stored.

`attic init` and `attic clone` set the label automatically from the host repo's origin remote, as `owner/repo` — the unambiguous name (two repos both called `attic` under different owners no longer collide). No origin? It falls back to the repo basename. Override any time:

```sh
cd ~/git/wifimgr
attic label get              # -> ravinald/wifimgr (from origin; basename if no origin)
attic label set wifimgr      # override to a name of your choice (marks it manual)
```

`attic list` then reads by name instead of by SHA:

```
LABEL              FP            HOST ROOT                BRANCH             SYNC
ravinald/wifimgr   8b88ecad3aa9  /Users/you/git/wifimgr   host/8b88ecad3aa9  clean
ravinald/attic     3f2a9c1d5e7b  /Users/you/git/attic     host/3f2a9c1d5e7b  ↑1 ↓0
```

### Keeping the map honest: `attic doctor`

Origins move — a repo gets renamed, transferred to an org, re-homed. `attic doctor` sweeps every overlay on the machine and flags any whose label no longer matches its origin's `owner/repo`:

```sh
attic doctor                 # report only; exits non-zero if fixable drift exists (hook-friendly)
attic doctor --fix           # rewrite auto-derived labels + refresh moved origins in local meta
attic doctor --fix --force   # also adopt the origin slug over a label you set by hand
attic doctor --fix --push    # ...and publish the corrected map to each affected mono remote
```

A label you set by hand is never overwritten without `--force` — provenance is tracked in `meta.toml` (`label_source = origin|manual`). Plain `--fix` stays local, so hooks and offline runs never touch the network; add `--push` to chain `attic labels push` for the mono remotes whose overlays changed.

Labels live in each overlay's local `meta.toml`, so they don't travel with the overlay's files. Sync them across machines over the mono remote's `_attic/labels` branch, which holds a single flat `labels.toml` map:

```sh
attic labels push            # publish this machine's fp -> label map
attic labels pull            # on another machine, apply the published names
```

Two caveats worth knowing:

- Labels **don't rename the `host/<fp>` branches** on the remote. The fingerprint stays the branch name — it's the stable, per-clone identity; a label is mutable and per-user, so it'd be a poor branch name. To decode branches from the GitHub UI, browse the `_attic/labels` branch: `attic labels push` writes a `README.md` there — a table linking each `owner/repo` label to its `host/<fp>` branch — alongside the raw `labels.toml` key.
- Labels sync is **mono-mode only**. Per-host remotes have nothing shared to publish the map to.

## Guardrails on the mono remote

A mono remote is an overlay store, not a repo you open PRs against — every `host/<fp>` branch is an independent orphan history, and merging one into another corrupts a repo's overlay. The enforcement is an auto-close-PR Action (a merge-blocking ruleset can't work here — it also blocks `attic push`); see [`docs/mono-remote-guardrails.md`](docs/mono-remote-guardrails.md) for the reasoning and surface-reduction settings, with copy-paste templates under [`examples/mono-remote/`](examples/mono-remote/).

## The `.gitignore` contract

`attic add` writes (and `attic rm` removes) entries inside a clearly-attributed block:

```
# BEGIN attic — managed by `attic`, do not edit between markers
CLAUDE.md
docs-internal/
# END attic
```

Edit the block by hand only at your own risk — `attic add`/`rm` will rewrite it. Content outside the markers is preserved.

The block alone isn't enough: `.gitignore` only suppresses *untracked* paths, so it can't untrack a path already in the host index or stop a `git add -f`. So `attic add` also runs `git rm --cached` against the host after adopting a path, and `attic clone` rewrites the block on restore. If a path is already stuck in the host index (a stray force-add, a pre-`attic` commit), `attic eject` evicts it — working-tree files and overlay history stay put. `attic eject --check` reports without changing, so a pre-commit hook can gate on it.

## Why not vcsh / repoverlay / chezmoi?

- **`vcsh`** is the same mechanism but `$HOME`-flavoured: no concept of a host repo, you key overlays by name and you remember those names on every machine. `attic` keys by the host repo's root commit and auto-attaches based on cwd.
- **`repoverlay`** solves the inverse problem: pull *shared* overlays (editor configs) into many host repos. Useful, but not for "version-control this one repo's private docs."
- **`chezmoi`/`yadm`** are dotfile managers — wrong scope.

## Build / quality

```sh
make build       # bin/attic
make test        # go test ./...
make lint        # golangci-lint
make vuln        # govulncheck
make check       # all of the above
```

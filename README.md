# attic

Track files alongside a git repo without committing them to it.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
Status: **alpha**, latest release `v0.2.0`. See [CHANGELOG.md](CHANGELOG.md).

`attic` keeps a per-host-repo *bare* git overlay whose git directory sits outside the host work tree. Overlay files stay in the repo where you actually use them, while their history lives elsewhere on its own remote — and a marker block in the host's `.gitignore` stops them ever leaking upstream.

## What goes in an overlay

Anything that belongs *with* a repo but not *in* it — the files you lose on a fresh clone and would rather not justify in a PR:

- **Notes and working docs** — design scratch, a running TODO, investigation logs, whatever you'd keep in a `notes/` or `docs-internal/` directory.
- **Local dev config** — an `.envrc` for direnv, editor or debugger launch settings, a `Makefile.local` with per-machine paths.
- **AI assistant instructions** — a project-local `CLAUDE.md`, `AGENTS.md`, or `.cursorrules` that encodes how *you* work rather than house style.
- **Fork-local scripts and patches** — helpers you never intend to upstream, on a repo you don't control.
- **Employer or client context on a public repo** — the notes you can't commit to an open-source project.
- **Build output worth keeping** — benchmark baselines or profiles you want on every machine without bloating the repo.

An overlay is an ordinary git repo on an ordinary remote, so treat it like any private repo: right for notes and config, wrong for secrets. Keep credentials in a secrets manager and have the overlay reference them, not carry them.

## How it works

The trick is `vcsh`'s, just per-project: `git --git-dir=<bare-outside-the-repo> --work-tree=<host-repo-root>`. Every `attic` subcommand is a thin wrapper that fills in those flags.

State lives at:

```
~/.local/share/attic/
  repos/<root-commit-sha-12>/
    attic.git/     # the bare overlay
    meta.toml      # host_root, host_name, label, remote, branch, mono, created_at

~/.config/attic/
  config.toml      # global settings
  overrides.toml   # per-machine label overrides, never pushed
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

### Mono remote (recommended) — one shared repo, branch per repo

```sh
# One-time setup: a single private repo holds every overlay you'll ever have.
gh repo create you/attic-overlays --private

# In each host repo:
cd ~/git/myproject
attic init --mono-remote git@github.com:you/attic-overlays.git
attic add notes/ .envrc
attic commit -m "myproject notes + local env"
attic push                       # → branch repo/<fingerprint>
```

On another machine, after cloning the host repo:

```sh
cd ~/git/myproject
attic clone --mono               # defaults to this machine's sole mono remote
ls notes/                        # back
```

The fingerprint (host repo's root commit SHA) is the branch name on the shared remote, so `attic clone --mono` works across all your projects without remembering names or URLs. Pass the URL explicitly if this machine tracks more than one mono remote.

### Per-host remote — one private GitHub repo per project

```sh
cd ~/git/myproject
attic init --gh-private          # creates you/myproject-attic via gh
attic add notes/
attic commit -m "first overlay"
attic push
```

On another machine:

```sh
cd ~/git/myproject
attic clone git@github.com:you/myproject-attic.git
```

## Commands

| Command | Purpose |
|---|---|
| `attic init [--remote URL \| --mono-remote URL \| --gh-private]` | Create overlay for the current host repo. |
| `attic deinit [--force]` | Remove the local overlay + `.gitignore` block (work-tree files stay). Refuses to drop unpushed commits without `--force`. |
| `attic clone [remote] [--mono] [--force]` | Restore an existing overlay on a new machine. With `--mono`, the remote defaults to this machine's sole mono remote. Refuses to clobber existing files without `--force`. |
| `attic add <path>... [--on-duplicate off\|warn\|manage]` | **Register** paths: append to host `.gitignore` block and stage. Warns when a path is already registered or already covered by a broader entry, and leaves the block unchanged in that case. `--on-duplicate` overrides the policy for redundant outside rules (see below). |
| `attic stage [<path>...]` | **Re-stage** new and modified files under already-registered paths, without touching the `.gitignore` block. No arguments stages every managed entry. Refuses paths the block doesn't cover. |
| `attic rm <path>... [--delete]` | Stop tracking; `--delete` also removes the file. |
| `attic config get\|set\|list [--global] <key> [value]` | Read/write settings (`gitignore.on_duplicate`, `status.ignore`). `set` targets the current repo, or `--global` (`~/.config/attic/config.toml`). |
| `attic eject [--check]` | Evict managed paths from the **host** index (never from disk or the overlay); `--check` reports without changing. |
| `attic commit [-m <msg>] [-a] [--allow-empty]` | Commit staged overlay changes. Without `-m`, uses a timestamped snapshot message; `-a` also stages edits to tracked files. |
| `attic status` | `git status` for the overlay, plus overlay files the host `.gitignore` hides from git (minus any `status.ignore` patterns). |
| `attic push` `pull` `fetch` `log` `diff` | Pass-through to git. |
| `attic sync [--strategy=rebase\|merge]` | Fetch + integrate + push. Refuses on a dirty index or edits to overlay-tracked files; untracked host files are ignored. |
| `attic ls` | List paths tracked in the overlay. |
| `attic list [--fetch] [--wide] [--json]` | Show every overlay on this machine with label, fp, sync state. |
| `attic where [--fp]` | Print bare path, fingerprint, remote. |
| `attic label get` | Print the display name: local override, else shared/auto name, else basename. |
| `attic label set <name>` / `--unset` | Set (or clear) a per-machine display override in `~/.config/attic` — never pushed. |
| `attic label reset [--force]` | Clear ALL local overrides on this machine (force-reset to shared/auto names); lists them without `--force`. |
| `attic labels edit [--remote URL]` | Edit the whole shared map in `$EDITOR`; validates, regenerates the README, publishes on save. |
| `attic labels push` / `attic labels pull` `[--remote URL]` | Contribute new overlays to the map (never overwrites) / cache the map's names locally. `--remote` disambiguates when the machine has several mono remotes. |
| `attic doctor [--fix] [--force] [--push]` | Audit every overlay's label against its origin remote; report drift, `--fix` corrects it, `--push` publishes the corrected map. Also reports overlays orphaned by a host history rewrite. |
| `attic rekey [--dry-run]` | Re-point an overlay orphaned by a host history rewrite onto the repo's current fingerprint. Moves storage, renames the `repo/<fp>` branch, rewires config and meta; never rewrites overlay history. |
| `attic exec -- <git-args>` | Run any git command against the overlay. |
| `attic version` | Version, commit, build date. |

## Labels: naming your overlays

An overlay's identity is the host repo's root-commit SHA — stable, but unreadable. On the mono remote every project is a branch named `repo/<fingerprint>`:

```
repo/8b88ecad3aa9
repo/3f2a9c1d5e7b
repo/a1b2c3d4e5f6
```

Nothing there tells you which branch is `myproject` and which is `otherproject`. A **label** is a fingerprint → human-name mapping that makes the listing legible without changing where anything is stored.

Labels live in two layers, and no machine "owns" an overlay:

- **The shared map** — `labels.toml` on the mono remote's `_attic/labels` branch. The canonical fingerprint → name mapping everyone browses. `attic init`/`clone` seed it from the host origin as `owner/repo`; `attic labels edit` is the one authority for changing an existing name.
- **Local overrides** — `~/.config/attic/overrides.toml`. Per-machine display names that **never leave the machine**. `attic label set` writes here.

`attic list` and `attic label get` resolve **override → shared/auto name → repo basename**:

```sh
cd ~/git/myproject
attic label get              # -> you/myproject (from origin; basename if no origin)
attic label set proj         # a display name for THIS machine only (never pushed)
attic label set --unset      # drop the override, fall back to the map/auto name
```

`attic list` then reads by name instead of by SHA:

```
LABEL             FP            HOST ROOT                    BRANCH             SYNC
you/myproject     8b88ecad3aa9  /Users/you/git/myproject     repo/8b88ecad3aa9  clean
you/otherproject  3f2a9c1d5e7b  /Users/you/git/otherproject  repo/3f2a9c1d5e7b  ↑1 ↓0
```

### Keeping the map honest: `attic doctor`

Origins move — a repo gets renamed, transferred to an org, re-homed. `attic doctor` sweeps every overlay on the machine and flags any whose label no longer matches its origin's `owner/repo`:

```sh
attic doctor                 # report only; exits non-zero if fixable drift exists (hook-friendly)
attic doctor --fix           # rewrite auto-derived labels + refresh moved origins in local meta
attic doctor --fix --force   # also adopt the origin slug over a label you set by hand
attic doctor --fix --push    # ...and publish the corrected map to each affected mono remote
```

`doctor` reconciles only the auto (origin-derived) label in local `meta.toml`; it never touches a curated name in the shared map (push is contribute-only). An overlay with a **local override** is reported as `overridden` and left entirely alone — doctor honours your local choice. To hand it back to doctor, clear the override with `attic label reset`. Plain `--fix` stays local, so hooks and offline runs never touch the network; add `--push` to chain `attic labels push` for the mono remotes whose overlays changed.

Three commands manage the shared map on `_attic/labels`:

```sh
attic labels edit            # open the whole map in $EDITOR; validate + publish on save
attic labels push            # contribute NEW overlays' auto names; never overwrites existing entries
attic labels pull            # cache the map's names into local meta for display (an override still wins)
```

`attic labels edit` is the authority — it's the only way to rename an existing entry, including a "foreign" overlay whose host repo lives on another machine. It edits a simple `<fingerprint>  <label>` table; deleting a line drops that entry. `attic labels push` is **contribute-only**: it fills in overlays the map hasn't seen yet, so no machine's push can clobber a curated name — the map stays the source of truth. A `--mono-remote` init seeds this branch and points the repo's default branch at it, so the map is the landing page from day one.

Two caveats worth knowing:

- Labels **don't rename the `repo/<fp>` branches** on the remote. The fingerprint stays the branch name — it's the stable, per-clone identity; a label is mutable and per-user, so it'd be a poor branch name. To decode branches from the GitHub UI, browse the `_attic/labels` branch: `attic labels push` writes a `README.md` there — a table linking each `owner/repo` label to its `repo/<fp>` branch — alongside the raw `labels.toml` key.
- Labels sync is **mono-mode only**. Per-host remotes have nothing shared to publish the map to.

## Guardrails on the mono remote

A mono remote is an overlay store, not a repo you open PRs against — every `repo/<fp>` branch is an independent orphan history, and merging one into another corrupts a repo's overlay. The enforcement is an auto-close-PR Action (a merge-blocking ruleset can't work here — it also blocks `attic push`); see [`docs/mono-remote-guardrails.md`](docs/mono-remote-guardrails.md) for the reasoning and surface-reduction settings, with copy-paste templates under [`examples/mono-remote/`](examples/mono-remote/).

## The `.gitignore` contract

`attic add` writes (and `attic rm` removes) entries inside a clearly-attributed block:

```
# BEGIN attic — managed by `attic`, do not edit between markers
.envrc
notes/
# END attic
```

Edit the block by hand only at your own risk — `attic add`/`rm` will rewrite it. Content outside the markers is preserved.

### Redundant rules outside the block

If a path you `attic add` is already ignored by a rule *outside* the block (a `notes/` line you added by hand before adopting attic), `on_duplicate` governs what happens:

| Mode | Behavior |
|---|---|
| `off` | Add to the block, leave the outside rule alone. |
| `warn` *(default)* | Add to the block, print which outside rule is now redundant. |
| `manage` | Add to the block **and delete** the redundant outside rule so the block is the single source. |

Precedence, highest first: `--on-duplicate` flag › `ATTIC_GITIGNORE_ON_DUPLICATE` env › per-repo (`attic config set gitignore.on_duplicate …`) › global (`--global`) › `warn`. Only slash-equivalent, glob-free rules qualify for `manage` — attic never second-guesses a real pattern like `*.local`, and never touches lines inside another tool's markers.

`warn` speaks only for paths a run actually adopts, so re-running `attic add notes/` to stage new files under an already-managed directory stays quiet. `manage` still scans every path, so switching to it after the fact absorbs a rule an earlier `off`/`warn` run left behind.

The block alone isn't enough: `.gitignore` only suppresses *untracked* paths, so it can't untrack a path already in the host index or stop a `git add -f`. So `attic add` also runs `git rm --cached` against the host after adopting a path, and `attic clone` rewrites the block on restore. If a path is already stuck in the host index (a stray force-add, a pre-`attic` commit), `attic eject` evicts it — working-tree files and overlay history stay put. `attic eject --check` reports without changing, so a pre-commit hook can gate on it.

### The overlay's exclude file

The overlay's work tree is the *whole* host repo, so by default every host file reads as untracked to it. attic writes `/*` into a marker block in the overlay's `info/exclude` to suppress that; the `git add --force` behind `attic add` outranks it, so adopting a path still works. Overlays created before this existed are healed on the next command.

That cuts the other way for the paths the overlay owns: the host `.gitignore` outranks `info/exclude`, so git will never volunteer a *new* file under `notes/` — not in `git status`, not with `-uall`. `attic status` asks for those by name and lists them separately, which is the only reason a file you just wrote doesn't sit there unnoticed until you wonder why it never pushed.

### Quieting that list: `status.ignore`

Asking by name is unconditional — attic queries `git ls-files --others --ignored`, and every file under overlay scope is ignored by construction. So no `.gitignore` or `info/exclude` rule can trim the list; adding one only makes a file *more* certainly ignored, hence *more* certainly reported. Finder droppings pile up there and a genuinely new file gets skimmed past.

`status.ignore` filters after the query:

```sh
attic config set --global status.ignore '.DS_Store,Thumbs.db'
```

| Pattern form | Matches |
|---|---|
| `.DS_Store`, `*.tmp` | the **basename**, at any depth |
| `scratch/` | everything under a directory of that name, at any depth |
| `notes/*.tmp` | the whole host-relative path |

`**/.DS_Store` is accepted as a synonym for the basename form. attic ships **no** default patterns — a filter that hides a file the overlay should have adopted is worse than three lines of `.DS_Store`, so you opt in.

Layers **union** rather than override: `ATTIC_STATUS_IGNORE` env + per-repo (`attic config set status.ignore …`) + global. A per-repo pattern adds to the global list instead of replacing it, which is what setting one asks for. `attic config list` shows each layer and the effective set. A malformed pattern warns on stderr and hides nothing.

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

# Architecture

`attic` is a thin Go wrapper around an old git trick: bare repo outside the work tree, every operation runs as `git --git-dir=<bare> --work-tree=<host-root> <subcommand>`. The same trick `vcsh` uses for `$HOME`, applied per-project.

## On-disk layout

```
~/.local/share/attic/                     # XDG_DATA_HOME respected
  repos/
    <root-commit-fp>/                     # 12-char prefix of host repo's root commit SHA
      attic.git/                          # bare overlay
      meta.toml                           # host_root, branch, remote, mono flag
```

`<root-commit-fp>` is **stable across machines and clones** — the same host repo on three laptops resolves to the same fingerprint, hence the same overlay storage path on each, hence the same overlay branch on the remote. Multi-root repos sort their root commits and take the smallest SHA.

## Two remote shapes

Pick one per host repo. Stored in `meta.toml` as `mono = true|false`.

### Per-host (`--remote URL` / `--gh-private`)

One private remote per host repo. Branch is `main`. Pushes go straight to `origin/main`.

```
git@github.com:you/wifimgr-attic.git       (origin/main)
git@github.com:you/netbox-attic.git        (origin/main)
```

Pro: each overlay is independent. Con: clutter — N overlays = N private repos.

### Mono (`--mono-remote URL` / `--mono`)

One private remote shared across **all** your overlays. Each host repo's overlay lives on its own branch named `host/<fp>`.

```
git@github.com:you/attic-overlays.git
  branches:
    host/a49bee3fa207   ← wifimgr's overlay
    host/7c4696d0cdcf   ← netbox's overlay
```

Pro: one repo to bootstrap, one URL to remember. Branch names are SHAs so no project names leak. Con: GitHub's branch-switcher UI is awkward when browsing.

In mono mode, `init` configures `push.default = current` and `push.autoSetupRemote = true` so plain `attic push` always routes to the matching branch and creates it on first push.

### Labels on the mono remote

`attic` reserves a single orphan branch on the mono remote, `_attic/labels`, carrying one TOML file:

```
_attic/labels
  labels.toml
```

Format:

```toml
[hosts.a49bee3fa207]
label = "wifimgr"

[hosts.7c4696d0cdcf]
label = "netbox"
```

The `_attic/` prefix segregates the file from the `host/<fp>` overlay branches and from anything you might create by hand. `attic labels push` reads the local `Label` field from every `meta.toml` pointing at this remote, merges with what's already published, and force-creates a commit on `_attic/labels`. `attic labels pull` does the reverse and updates each local overlay whose fingerprint matches an entry. The branch is optional — local `meta.toml` always wins for the machine you're on, and `attic list` works fine without it.

### Multiple machines, same host repo

A host repo cloned on two machines has the same root commit, the same fingerprint, and therefore the **same** `host/<fp>` branch on the mono remote. There is no per-machine branch. Day-to-day this is just normal git collaboration on one branch — push from work, pull on home, commit, push back.

Use `attic sync` for the steady-state loop (`fetch` + `rebase` + `push`, refuses dirty work tree). For the trickier first-time case where both machines already have a populated overlay path *before* either has pushed, see [two-machine-bootstrap.md](two-machine-bootstrap.md).

## The `.gitignore` contract

When you `attic add <path>`, the path enters a marker-delimited block in the **host repo's** `.gitignore`:

```
# BEGIN attic — managed by `attic`, do not edit between markers
CLAUDE.md
docs-internal/
# END attic
```

This guards against the overlay path accidentally leaking into the upstream history. `attic add`/`attic rm` rewrite the block; content outside the markers is preserved.

Because the path is now gitignored, `git add` would refuse to track it. `attic add` runs `git add --force` against the overlay — the force is intentional: gitignored upstream **and** tracked in the overlay is the design.

## Host repo identity (root commit SHA)

Every git repo has at least one root commit (a commit with no parents). For a normal linear repo there's exactly one, and it's the same on every clone. `attic` keys overlay storage off this:

```
git rev-list --max-parents=0 HEAD   →   sort   →   first 12 chars
```

Why the root commit and not `origin` URL? Because URLs change (ssh ↔ https, mirrors, forks) but the root commit doesn't. A repo on GitHub, GitLab, and a USB stick all have the same root commit; they all map to the same overlay.

Edge cases:
- **Multi-root repos** (uncommon — typically arise from `git merge --allow-unrelated-histories`): take the smallest SHA. Stable.
- **Empty repo** (no commits): `attic init` errors out. Make at least one commit first.
- **Repo with rewritten root** (`git rebase` rewrote the original root commit): fingerprint changes; previous overlay is orphaned. Workaround: `mv ~/.local/share/attic/repos/<old>/ ~/.local/share/attic/repos/<new>/` and update `meta.toml`. Rare.

## Why not …

- **`vcsh`** is the same mechanism, but `$HOME`-flavoured: no host-repo identity, no auto-attach to cwd, no `.gitignore` guard. `attic` is `vcsh` retargeted at per-project work trees.
- **`tylerbutler/repoverlay`** solves the inverse problem: pull *shared* overlays (editor configs, lint settings) into many host repos via symlinks. Useful, different.
- **`chezmoi`/`yadm`** are dotfile managers — wrong scope.
- **`git submodule`/`subtree`** make the embedded repo *part of* the host history. Defeats the point.

## Code layout

```
cmd/attic/main.go                 # entry, calls cmd.Execute
internal/cmd/                     # cobra commands, one file per command
  root.go                         # root command + Execute()
  init.go clone.go                # state-creating
  add.go rm.go                    # gitignore-block-aware
  commit.go ls.go where.go exec.go
  passthrough.go                  # status/push/pull/fetch/log/diff
  version.go common.go
internal/host/detect.go           # find host root, root commit SHA, origin URL; symlink-canonical
internal/store/                   # XDG-aware paths + meta.toml
internal/gitwrap/git.go           # exec git with --git-dir/--work-tree (skipped when empty)
internal/ignore/block.go          # marker-block splice in host .gitignore (atomic write)
internal/gh/create.go             # best-effort `gh repo create` integration
```

Each `internal/` package earns its keep with either a non-trivial type (`gitwrap.Repo`, `store.Meta`, `ignore.Block`) or three-plus exports.

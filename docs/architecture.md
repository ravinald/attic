# Architecture

`attic` is a thin Go wrapper around an old git trick: bare repo outside the work tree, every operation runs as `git --git-dir=<bare> --work-tree=<host-root> <subcommand>`. The same trick `vcsh` uses for `$HOME`, applied per-project.

## On-disk layout

```
~/.local/share/attic/                     # XDG_DATA_HOME respected
  repos/
    <root-commit-fp>/                     # 12-char prefix of host repo's root commit SHA
      attic.git/                          # bare overlay
      meta.toml                           # fingerprint, host_root, host_name, label,
                                          # label_source, origin_url, remote, branch,
                                          # mono, created_at, gitignore_on_duplicate

~/.config/attic/                          # XDG_CONFIG_HOME respected
  config.toml                             # global settings (gitignore.on_duplicate)
  overrides.toml                          # per-machine label overrides, never pushed
```

Data and config split on purpose: everything under `repos/` is reconstructible from a remote,
while `overrides.toml` is deliberately machine-local and has nowhere to sync to.

`<root-commit-fp>` is **stable across machines and clones** — the same host repo on three laptops resolves to the same fingerprint, hence the same overlay storage path on each, hence the same overlay branch on the remote. Multi-root repos sort their root commits and take the smallest SHA.

## Two remote shapes

Pick one per host repo. Stored in `meta.toml` as `mono = true|false`.

### Per-host (`--remote URL` / `--gh-private`)

One private remote per host repo. Branch is `main`. Pushes go straight to `origin/main`.

```
git@github.com:you/myproject-attic.git      (origin/main)
git@github.com:you/otherproject-attic.git   (origin/main)
```

Pro: each overlay is independent. Con: clutter — N overlays = N private repos.

### Mono (`--mono-remote URL` / `--mono`)

One private remote shared across **all** your overlays. Each host repo's overlay lives on its own branch named `repo/<fp>`.

```
git@github.com:you/attic-overlays.git
  branches:
    repo/a49bee3fa207   ← myproject's overlay
    repo/7c4696d0cdcf   ← otherproject's overlay
```

Pro: one repo to bootstrap, one URL to remember. Branch names are SHAs so no project names leak. Con: GitHub's branch-switcher UI is awkward when browsing.

In mono mode, `init` configures `push.default = current` and `push.autoSetupRemote = true` so plain `attic push` always routes to the matching branch and creates it on first push.

#### Fetch scope

Each `repo/<fp>` branch is an independent orphan history, so an overlay has no reason to hold any branch but its own. `init` and `clone` therefore pin the fetch refspec to a single branch:

```
remote.origin.fetch = +refs/heads/repo/<fp>:refs/remotes/origin/repo/<fp>
```

Git's default (`+refs/heads/*:refs/remotes/origin/*`, what `remote add` writes) makes a bare `attic fetch` or `attic pull` download the entire store into that one overlay's bare — every unrelated project's history, once per overlay on the machine. On a store of 18 overlays that turned a 31 KiB restore into a 45 MiB one.

`attic sync` re-pins the refspec when it finds a mono overlay wired with the wildcard, so overlays created before this healed on their next sync. Per-host overlays keep the wildcard: their bare *is* the whole overlay, so scoping it would be wrong.

Nothing else in the mono path needs the wide refspec. `attic sync` fetches an explicit branch (`fetch origin <branch>`), and the label commands work through a throwaway clone of `_attic/labels`, never the overlay bare.

### Labels on the mono remote

`attic` reserves a single orphan branch on the mono remote, `_attic/labels`, carrying the map and a
rendering of it:

```
_attic/labels
  labels.toml       # the map itself
  README.md         # generated table: label → repo/<fp> branch → commit log → fingerprint
```

Format:

```toml
[hosts.a49bee3fa207]
label = "myproject"

[hosts.7c4696d0cdcf]
label = "otherproject"
```

The `_attic/` prefix segregates these files from the `repo/<fp>` overlay branches and from anything you might create by hand.

Names resolve through two layers, so no machine owns an overlay:

| Layer | Location | Scope |
|---|---|---|
| Local override | `~/.config/attic/overrides.toml` | This machine only, never pushed. Written by `attic label set`. |
| Shared map | `labels.toml` on `_attic/labels` | Canonical across machines. Renamed only by `attic labels edit`. |

`attic label get` and `attic list` resolve **override → shared/auto name → repo basename**.

Three commands move the shared map:

- **`attic labels push`** collects the local `Label` from every `meta.toml` pointing at this remote and merges into what's published — **contribute-only**: a fingerprint already in the map is left alone. That's what stops one machine's push clobbering a curated name, and it means `push` needs no force.
- **`attic labels pull`** caches the map's names into local meta for display. An override still wins.
- **`attic labels edit`** opens the whole map in `$EDITOR` as a `<fingerprint>  <label>` table, then validates, regenerates `README.md`, and pushes on save. It is the only way to rename an existing entry — including a "foreign" overlay whose host repo lives on another machine — because it is the only writer allowed to overwrite.

All three run from anywhere: they use the current overlay's remote, fall back to the machine's sole mono remote, and take `--remote <url>` to disambiguate.

The branch is optional — local `meta.toml` always wins for the machine you're on, and `attic list` works fine without it.

`attic doctor` reconciles the other direction: it compares each overlay's label against its host repo's origin slug and reports drift, since origins get renamed and transferred. It only ever writes the auto (origin-derived) label in local `meta.toml`; a curated name in the shared map and an overlay carrying a local override are both left alone. `--push` chains `labels push` for the affected mono remotes.

### Multiple machines, same host repo

A host repo cloned on two machines has the same root commit, the same fingerprint, and therefore the **same** `repo/<fp>` branch on the mono remote. There is no per-machine branch. Day-to-day this is just normal git collaboration on one branch — push from work, pull on home, commit, push back.

Use `attic sync` for the steady-state loop (`fetch` + `rebase` + `push`, refuses dirty work tree). For the trickier first-time case where both machines already have a populated overlay path *before* either has pushed, see [two-machine-bootstrap.md](two-machine-bootstrap.md).

## The `.gitignore` contract

When you `attic add <path>`, the path enters a marker-delimited block in the **host repo's** `.gitignore`:

```
# BEGIN attic — managed by `attic`, do not edit between markers
.envrc
notes/
# END attic
```

This guards against the overlay path accidentally leaking into the upstream history. `attic add`/`attic rm` rewrite the block; content outside the markers is preserved.

Because the path is now gitignored, `git add` would refuse to track it. `attic add` runs `git add --force` against the overlay — the force is intentional: gitignored upstream **and** tracked in the overlay is the design.

### Keeping the host index clean

The `.gitignore` block is necessary but not sufficient: git only ignores *untracked* paths, so a rule can neither untrack a path already in the host index nor stop a `git add -f`. If a path was force-added or committed to the host before attic adopted it, the block is inert and the path stays staged upstream — the pre-commit guard then refuses every commit until it's cleared.

So adoption also evicts. `attic add` runs `git rm --cached --ignore-unmatch` against the **host** repo for each adopted path (via `git -C <host-root>`, worktree-safe). `--cached` leaves the working-tree files in place — the overlay still owns them — while removing them from the host index. `attic clone` does the same after checkout so a second machine's restored files land ignored, not staged.

`attic eject` repairs an already-contaminated repo: it evicts every managed path (the union of the ignore-block entries and the overlay's tracked files) from the host index. `attic eject --check` reports without changing anything, exiting non-zero when a managed path is staged as a host addition — the form a pre-commit guard calls.

### The overlay's `info/exclude`

The overlay's work tree is the *whole* host repo, so with no exclusions every host file reads as untracked to it: `attic status` buries the one real change under the host's entire tree, and `attic commit` dies with git's "nothing added to commit but untracked files present". attic writes `/*` into a marker block in the overlay's `info/exclude` to suppress that. The `git add --force` in `attic add` outranks it, so adopting a path still works. `openOverlay` heals overlays created before this existed, rather than only `init` — it's the one code path every overlay command passes through.

That cuts the other way for the paths the overlay owns. The host `.gitignore` outranks `info/exclude`, so git will never volunteer a *new* file under `notes/` — not in `git status`, not with `-uall`. `attic status` therefore asks for them by name (`ls-files --others --ignored --exclude-standard` scoped to overlay-owned paths) and prints them in a separate section, suppressed under `--porcelain`/`-s`/`-z` so a parsing caller sees only git's own stream.

## Host repo identity (root commit SHA)

Every git repo has at least one root commit (a commit with no parents). For a normal linear repo there's exactly one, and it's the same on every clone. `attic` keys overlay storage off this:

```
git rev-list --max-parents=0 HEAD   →   sort   →   first 12 chars
```

Why the root commit and not `origin` URL? Because URLs change (ssh ↔ https, mirrors, forks) but the root commit doesn't. A repo on GitHub, GitLab, and a USB stick all have the same root commit; they all map to the same overlay.

Edge cases:
- **Multi-root repos** (uncommon — typically arise from `git merge --allow-unrelated-histories`): take the smallest SHA. Stable.
- **Empty repo** (no commits): `attic init` errors out. Make at least one commit first.
- **Repo with rewritten root**: the fingerprint changes and the previous overlay is orphaned — every attic command reports `no overlay for <path>` while the history sits intact on disk and on the remote. Fix it with **`attic rekey`** (run inside the host repo), which moves the storage dir, renames the `repo/<fp>` branch, rewrites the branch config and fetch refspec, and updates `meta.toml`. `attic doctor` finds orphans across every overlay on the machine; it reports them and never re-keys as part of `--fix`, because moving storage wants the operator present.

  Anything that rewrites the root commit does this, not just `git rebase`: `git filter-repo` and `filter-branch` (purging a path that exists in the root commit rewrites it and every descendant), a squashed root, and grafted or amended history. Purging a directory from a repo's history is the common trigger, and it is worth knowing before the rewrite rather than after, because a `git reset --hard` onto the rewritten history deletes every overlay file the host index also tracked.

  Re-keying never rewrites overlay history; it only re-labels where that history is filed. The old `repo/<fp>` branch is left on the mono remote as a fallback — publish the new one with `attic push`.

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
  init.go clone.go deinit.go      # state-creating and -destroying
  add.go rm.go eject.go           # gitignore-block-aware; eject clears the host index
  commit.go status.go sync.go     # status/sync surface the overlay's own view
  ls.go list.go where.go exec.go
  label.go labels_edit.go         # label resolution + the _attic/labels map
  doctor.go                       # label-vs-origin drift audit
  config.go                       # config get/set/list across layers
  passthrough.go                  # push/pull/fetch/log/diff
  version.go
  common.go                       # host-git helpers: hostGit, ejectFromHost, topLevels,
                                  # overlayScope, ensureOverlayExclude, on_duplicate resolution
internal/host/detect.go           # find host root, root commit SHA, origin URL; symlink-canonical
internal/store/                   # XDG-aware paths, meta.toml, config.toml, overrides.toml
internal/gitwrap/git.go           # exec git with --git-dir/--work-tree (skipped when empty)
internal/ignore/                  # marker-block splice in host .gitignore (atomic write) +
                                  # duplicate detection for rules outside the block
internal/gh/create.go             # best-effort `gh` integration (repo create, default branch)
```

Each `internal/` package earns its keep with either a non-trivial type (`gitwrap.Repo`, `store.Meta`, `ignore.Block`) or three-plus exports.

# Troubleshooting

## `attic init` says "host: not inside a git repository"

`attic` operates on the git repo containing your cwd. `cd` into one first. If the directory IS a git repo, run `git status` to confirm git agrees.

## `attic init` says "repo has no commits"

Identity is the root commit SHA. A repo with zero commits has no root. Make a first commit (any commit), then `attic init`.

## `attic add` says "path X is outside host repo Y"

Most likely a symlink mismatch: macOS's `/var` is a symlink to `/private/var`, so `mktemp` paths and `git rev-parse --show-toplevel` can disagree on the canonical form. `attic` resolves both sides via `EvalSymlinks`, so this should be fixed — if you still see it, the path you're adding is genuinely outside the host repo (e.g. you typed `../other/file`).

## `attic clone --mono` says "no overlay branch host/<fp>"

The host repo on this machine has a fingerprint that doesn't exist on the mono remote. Causes:
- Overlay was never pushed from any other machine yet — run `attic init --mono-remote <url>` here instead.
- The host repo has been rebased to rewrite its root commit, so the fingerprint changed. List branches on the remote (`git ls-remote <url> 'host/*'`) and either rename one or start fresh.

## `git push` is rejected: "non-fast-forward"

Two machines pushed to the same `host/<fp>` branch without one pulling first. Standard git: `attic pull`, resolve any conflicts, `attic push` again.

## An overlay path keeps showing up staged in the host repo

A `.gitignore` rule can't untrack a path already in the host index or stop a `git add -f`, so once an overlay path lands in the host index (a stray force-add, a headless script, a pre-`attic` commit) it sticks and every commit trips the guard. Run `attic eject` from the host repo — it evicts every managed path from the host index while leaving the working-tree files and overlay history untouched. `attic eject --check` reports without changing anything; wire it into a pre-commit hook to catch the regression early.

## I committed an overlay path to the host repo by accident

Remove it from the host upstream (`git rm --cached path && git commit && git push`), then `attic eject` to keep the index clean going forward. The marker block in `.gitignore` plus `attic add`'s host-index eviction exist precisely to prevent this — check the block is intact and contains the path.

## I want to remove an overlay entirely

```sh
fp=$(attic where --fp)
rm -rf ~/.local/share/attic/repos/$fp
```

The marker block in the host's `.gitignore` stays; remove it by hand or with `attic rm` for each path before deleting the overlay state.

## My fingerprint changed because I rebased the root commit

Move the storage dir:

```sh
mv ~/.local/share/attic/repos/<old-fp>/ ~/.local/share/attic/repos/<new-fp>/
# edit meta.toml to update fingerprint and (for mono) branch name
```

For mono mode, also rename the branch on the remote:

```sh
attic exec -- push origin <old-branch>:host/<new-fp>
attic exec -- push origin --delete <old-branch>
```

## I want to use a different default branch name (not "main")

For per-host mode, set it manually after `init`:

```sh
attic exec -- symbolic-ref HEAD refs/heads/master   # for example
```

For mono mode, the branch name is derived from the fingerprint and shouldn't change.

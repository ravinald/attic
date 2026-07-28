# Troubleshooting

## `attic init` says "host: not inside a git repository"

`attic` operates on the git repo containing your cwd. `cd` into one first. If the directory IS a git repo, run `git status` to confirm git agrees.

## `attic init` says "repo has no commits"

Identity is the root commit SHA. A repo with zero commits has no root. Make a first commit (any commit), then `attic init`.

## `attic add` says "path X is outside host repo Y"

Most likely a symlink mismatch: macOS's `/var` is a symlink to `/private/var`, so `mktemp` paths and `git rev-parse --show-toplevel` can disagree on the canonical form. `attic` resolves both sides via `EvalSymlinks`, so this should be fixed — if you still see it, the path you're adding is genuinely outside the host repo (e.g. you typed `../other/file`).

## `attic clone --mono` says "no overlay branch repo/<fp>"

The host repo on this machine has a fingerprint that doesn't exist on the mono remote. Causes:
- Overlay was never pushed from any other machine yet — run `attic init --mono-remote <url>` here instead.
- The host repo has been rebased to rewrite its root commit, so the fingerprint changed. List branches on the remote (`git ls-remote <url> 'repo/*'`) and either rename one or start fresh.

## `git push` is rejected: "non-fast-forward"

Two machines pushed to the same `repo/<fp>` branch without one pulling first. Standard git: `attic pull`, resolve any conflicts, `attic push` again.

## An overlay path keeps showing up staged in the host repo

A `.gitignore` rule can't untrack a path already in the host index or stop a `git add -f`, so once an overlay path lands in the host index (a stray force-add, a headless script, a pre-`attic` commit) it sticks and every commit trips the guard. Run `attic eject` from the host repo — it evicts every managed path from the host index while leaving the working-tree files and overlay history untouched. `attic eject --check` reports without changing anything; wire it into a pre-commit hook to catch the regression early.

## I committed an overlay path to the host repo by accident

Remove it from the host upstream (`git rm --cached path && git commit && git push`), then `attic eject` to keep the index clean going forward. The marker block in `.gitignore` plus `attic add`'s host-index eviction exist precisely to prevent this — check the block is intact and contains the path.

## `attic status` says clean, but I know I added a file

The host `.gitignore` hides overlay-owned paths from git and outranks the overlay's own `info/exclude`, so git will never volunteer a *new* file under `notes/` — not in `git status`, not with `-uall`. `attic status` asks for those by name and prints them under **"Untracked overlay files"** below git's own output. If that section lists your file, it exists but was never staged: `attic add <path>`.

If you piped the command (`--porcelain`, `-s`, `-z`), that section is suppressed by design — it's prose, and it must not land in a stream a script parses. Run it bare to see it.

## `attic commit` says "nothing staged for commit"

Edits to already-tracked overlay files aren't staged automatically. Use `attic commit -a`. New files need `attic add` first — the error lists any it can see.

## `attic labels push` didn't rename my overlay

`push` is **contribute-only**: it fills in fingerprints the shared map has never seen and never overwrites an existing entry. That's deliberate — it's what stops one machine's push clobbering a name someone curated. To rename an existing entry, use `attic labels edit`, the only writer allowed to overwrite.

If the name is only wrong on *this* machine, you want a local override instead: `attic label set <name>` (never pushed).

## `attic doctor` reports an overlay as `overridden` and won't fix it

The overlay has a local override in `~/.config/attic/overrides.toml`, and doctor honours your local choice over the origin slug. To hand it back to doctor's auto reconciliation, clear the override with `attic label reset` (it lists what it would clear unless you pass `--force`).

## `attic labels` says "this machine has more than one mono remote"

The `labels` commands resolve the remote from the current overlay, then fall back to the machine's sole mono remote. With two or more, that fallback is ambiguous — name it: `attic labels push --remote git@github.com:you/attic-overlays.git`. Or run the command from inside a host repo whose overlay already points at the remote you mean.

## `attic add` warns that a `.gitignore` rule is now redundant

You hand-wrote an ignore rule for that path before adopting attic, so the managed block now shadows it. Harmless, but two sources of truth. Absorb the old rule:

```sh
attic config set gitignore.on_duplicate manage    # this repo
attic add notes/                                  # deletes the redundant outside rule
```

`manage` only touches slash-equivalent, glob-free rules — a real pattern like `docs-*` is never second-guessed, and lines inside another tool's markers are left alone. Silence the notice without changing anything with `off`.

## I want to remove an overlay entirely

```sh
attic deinit          # from inside the host repo
```

This deletes the bare overlay and its meta, and strips attic's block from the host `.gitignore`. Work-tree files stay on disk — `deinit` forgets how to track them, it doesn't delete them. It refuses when the overlay holds commits not on its remote; `attic push` first, or `--force` if you mean to drop them.

The remote side is untouched. For a mono remote, delete the branch yourself:

```sh
attic exec -- push origin --delete repo/$(attic where --fp)   # BEFORE deinit — needs the overlay
```

## My fingerprint changed because I rebased the root commit

Move the storage dir:

```sh
mv ~/.local/share/attic/repos/<old-fp>/ ~/.local/share/attic/repos/<new-fp>/
# edit meta.toml to update fingerprint and (for mono) branch name
```

For mono mode, also rename the branch on the remote:

```sh
attic exec -- push origin <old-branch>:repo/<new-fp>
attic exec -- push origin --delete <old-branch>
```

## I want to use a different default branch name (not "main")

For per-host mode, set it manually after `init`:

```sh
attic exec -- symbolic-ref HEAD refs/heads/master   # for example
```

For mono mode, the branch name is derived from the fingerprint and shouldn't change.

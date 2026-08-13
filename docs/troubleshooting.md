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

## `attic add` warned that a path is already registered

Two different intents share one verb's shape, so `add` distinguishes them rather than guessing:

- **`attic add <path>`** registers a path: it writes the path into the host `.gitignore` block and stages it. This is a one-time act per path.
- **`attic stage [<path>...]`** re-stages what the block already registers, and never touches the block. This is how new files under a managed directory reach the overlay index.

`attic add docs-internal` on an already-registered directory still stages it, so nothing breaks; the warning points at `attic stage` because that is the verb that says what you meant. `attic add docs-internal/new-file.md` warns and leaves the block **unchanged**: the `docs-internal` entry already covers the file, so a new line would ignore nothing further while making the block misreport the granularity the overlay is managed at.

Registering a file beneath an already-registered directory is the one case worth checking yourself. If the block genuinely should track that file rather than its parent directory, the parent entry is the thing to remove (`attic rm`), not the file to add.

## An overlay path keeps showing up staged in the host repo

A `.gitignore` rule can't untrack a path already in the host index or stop a `git add -f`, so once an overlay path lands in the host index (a stray force-add, a headless script, a pre-`attic` commit) it sticks and every commit trips the guard. Run `attic eject` from the host repo — it evicts every managed path from the host index while leaving the working-tree files and overlay history untouched. `attic eject --check` reports without changing anything; wire it into a pre-commit hook to catch the regression early.

## I committed an overlay path to the host repo by accident

Remove it from the host upstream (`git rm --cached path && git commit && git push`), then `attic eject` to keep the index clean going forward. The marker block in `.gitignore` plus `attic add`'s host-index eviction exist precisely to prevent this — check the block is intact and contains the path.

## `attic status` says clean, but I know I added a file

The host `.gitignore` hides overlay-owned paths from git and outranks the overlay's own `info/exclude`, so git will never volunteer a *new* file under `notes/` — not in `git status`, not with `-uall`. `attic status` asks for those by name and prints them under **"Untracked overlay files"** below git's own output. If that section lists your file, it exists but was never staged: `attic stage`. Use `attic stage`, not `attic add` — the file sits under a directory the block already registers, and `add` would append a redundant rule naming it.

If you piped the command (`--porcelain`, `-s`, `-z`), the header is dropped but the files still arrive, as `?? <path>` on git's own stream (`? <path>` under `--porcelain=v2`, NUL-terminated under `-z`). So `attic status --porcelain | wc -l` is a usable dirtiness check. Pass `--ignored` and attic stays quiet, because git's `!!` lines already cover the same files.

## `attic commit` says "nothing staged for commit"

Edits to already-tracked overlay files aren't staged automatically. Use `attic commit -a`. New files need staging first — `attic stage` for anything under a path the overlay already manages, `attic add <path>` only to register a path the block doesn't cover yet. The error lists any it can see.

## Every attic command says "no overlay for &lt;path&gt;" but the overlay existed yesterday

You rewrote the host repo's history. attic keys overlay storage by the host's root commit, so `git filter-repo`, `filter-branch`, a squashed or grafted root, or an amended root commit all move the key and orphan the overlay. The history is not lost: it is filed under the old fingerprint, on disk and on the remote.

Run **`attic rekey`** inside the host repo. It names both fingerprints, moves the storage dir, renames the `repo/<fp>` branch, rewrites the branch config and fetch refspec, and updates `meta.toml`. `--dry-run` prints the plan first. Then `attic push` to publish the new branch; the old one stays on the mono remote as a fallback.

Do **not** run `attic init` here. It would start an empty overlay beside the full one and leave the real history unreachable. The error says so, and refuses to offer init when it can see orphaned storage.

`attic doctor` reports orphans across every overlay on the machine, which is how you find one in a repo you haven't opened lately.

Two things to know before rewriting a host repo's history, because both bite after the fact rather than during:

- A `git reset --hard` onto the rewritten history **deletes every overlay file the host index also tracked**, since those paths are in the old index and absent from the new HEAD. Untracked overlay files survive. Recover with `attic rekey`, then `git --git-dir=$(attic where --fp | xargs -I{} echo ~/.local/share/attic/repos/{}/attic.git) --work-tree=. checkout -- <path>`.
- If a snapshot hook runs while the overlay is orphaned it fails silently and records nothing, so a change made in that window is only on disk. Commit it after re-keying.

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

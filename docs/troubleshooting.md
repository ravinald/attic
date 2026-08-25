# Troubleshooting

## `attic init` says "host: not inside a git repository"

`attic` operates on the git repo containing your cwd. `cd` into one first. If the directory IS a git repo, run `git status` to confirm git agrees.

## `attic init` says "repo has no commits"

Identity is the root commit SHA. A repo with zero commits has no root. Make a first commit (any commit), then `attic init`.

## `attic add` says "path X is outside host repo Y"

Most likely a symlink mismatch: macOS's `/var` is a symlink to `/private/var`, so `mktemp` paths and `git rev-parse --show-toplevel` can disagree on the canonical form. `attic` resolves both sides via `EvalSymlinks`, so this should be fixed. If you still see it, the path you're adding is genuinely outside the host repo (you typed `../other/file`, say).

## `attic clone --mono` says "no overlay branch `repo/<fp>`"

The host repo on this machine has a fingerprint that doesn't exist on the mono remote. Causes:

- The overlay was never pushed from any other machine yet. Run `attic init --mono-remote <url>` here instead.
- The host repo's root commit was rewritten, so the fingerprint changed. If the overlay still exists locally under the old fingerprint, `attic rekey` re-points it (see below). If it only exists on the remote, list the branches (`git ls-remote <url> 'repo/*'`), then `attic init --mono-remote <url>` here and fetch the old branch by name.

## `attic clone` says the remote "is a shared mono remote"

You passed a mono remote without `--mono`. Add the flag:

```sh
attic clone --mono https://github.com/you/attic-overlays/
```

Without it, clone takes the per-host-repo path and clones the whole bare, which is wrong in three ways at once: it downloads every project's overlay history, it lands on the remote's default branch (`_attic/labels`, not this repo's `repo/<fp>`), and it then checks that branch's `README.md` and `labels.toml` out into your host work tree. attic probes the remote for `repo/*` and `_attic/labels` refs before touching disk and refuses rather than guessing the mode, because `--mono` decides where files land and a typo'd URL must not silently change modes.

A clone that refuses leaves no overlay behind, so fix the flag and run it again. If you hit this on a version before the guard existed, the leftovers are a bare under `~/.local/share/attic/repos/<fp>/attic.git` with no `meta.toml` and HEAD on `_attic/labels`, plus `README.md` and `labels.toml` written into the host repo. `attic where` reports `(no overlay initialised)` while the bare exists, which is the signature. Recover by restoring the host file (`git checkout -- README.md`), deleting the stray `labels.toml`, removing that storage directory, and cloning again with `--mono`.

## `attic clone --force` says paths "are tracked by the host repo"

`--force` overwrites **untracked** files. A colliding path the host repo tracks already has an owner, and restoring an overlay over it destroys committed content, so no flag reaches those. The flag exists to reclaim stray untracked copies of overlay files, which is the collision a second machine produces.

If the path belongs to the overlay and the host repo tracks it by mistake, hand it over first with `git rm --cached <path>` plus a host commit, then clone.

## An overlay's bare is far larger than the files in it

`attic doctor` reports this across every overlay on the machine, and `attic doctor --fix` repairs it:

```text
STATUS        FP            LABEL              DETAIL
over-fetched  67c031190db7  ravinald/bodega    24 ref(s) from other projects in a 47M bare — `attic doctor --fix` drops them and repacks
```

A mono overlay should hold one branch. With git's default refspec (`+refs/heads/*:refs/remotes/origin/*`) one fetch pulls every project on the remote into this overlay's bare: measured at 48M against 496K for a correctly scoped sibling. `attic clone --mono` scopes the refspec, and `sync`, `fetch`, `pull` and `rekey` narrow it back if they find it widened, so a current binary stops the growth on its own.

Narrowing does not reclaim, and nothing collects it on its own. `git remote prune` drops only remote-tracking refs whose upstream branch is gone; these branches are alive on the remote and now sit outside the refspec, so fetch never considers them. Four overlays on one machine had a correctly scoped refspec and still carried 19, 19, 5 and 5 foreign refs at 46M, 46M, 38M and 30M.

To do it by hand, sweep **both** ref namespaces. `git clone --bare` writes foreign branches into `refs/heads/`, so clearing `refs/remotes/` alone leaves them holding every object reachable, and the repack then frees nothing while appearing to succeed:

```sh
fp=$(attic where --fp)
attic exec remote set-head origin --delete          # this one is a symref, not a plain ref
attic exec for-each-ref --format='%(refname)' refs/heads/ refs/remotes/ \
  | grep -v "repo/$fp$" \
  | while read -r r; do attic exec update-ref -d "$r"; done
attic exec reflog expire --expire=now --all
attic exec repack -a -d -l && attic exec prune --expire=now
```

Nothing dropped is lost: every one of those branches lives on the mono remote. The overlay's own branch and HEAD stay put, so its own commits survive whether they have been pushed or not. Confirm with `attic exec fsck --no-progress`, which stays silent when the symref was removed correctly.

## `attic pull`/`sync` says the overlay is mid-rebase

A previous `attic sync` rebase stopped on a conflict and nobody finished it. Every write command refuses until you do:

```text
attic: sync: overlay is mid-rebase, so this would write over an unresolved conflict.
Finish it with `attic exec rebase --continue`, or discard it with `attic exec rebase --abort`
```

`attic fetch`, `log`, `diff` and `exec` still work, so start there:

```bash
attic exec status   # which paths conflict
attic exec diff     # the conflict itself
```

**Check for commits on no other ref before choosing how to unwind it.** `--abort` resets the branch to where the rebase started, so it destroys anything committed since, and a snapshot hook driving the loop above commits once per run. The refusal counts them for you; `attic doctor` reports the same count. With none, `--abort` is the simple answer:

```bash
attic exec rebase --abort
```

With any, use `--quit`, which closes the operation and leaves the branch tip alone. It leaves HEAD detached, so re-point the branch yourself:

```bash
fp=$(attic where --fp)
attic exec rebase --quit
attic exec branch -f "repo/$fp" HEAD
attic exec symbolic-ref HEAD "refs/heads/repo/$fp"
```

Either way the two sides still have to reconcile, and `--abort` alone does not do it: it returns you to this machine's line, which is still missing whatever the remote added. Rebase onto the remote for real and resolve each conflict:

```bash
attic exec fetch origin
attic exec rebase "origin/repo/$fp"
# ...resolve, then:
attic exec add --force -- <path>
attic exec -c core.editor=true rebase --continue
attic sync
```

Two overlay files conflict most often: a dated changelog two machines both appended to, and a `.jsonl` ledger both appended to. Both are append-only by intent, so a hand-merged union beats letting either side win: keep every section from both, remote's first. Expect the conflicted file to contain nested conflict markers if the loop above ran, since it commits them; take the union from the two clean sides (`attic exec show :2:<path>` and `:3:<path>`) rather than untangling them.

The refusals exist because the recovery is not obvious from git's own message. Before them, `attic stage` would mark the conflicted path resolved with whatever was in the work tree and `attic commit` would bake that in, leaving a clean index over an open rebase. git then reported "nothing to commit, working tree clean" while every `sync` died on `fatal: It seems that there is already a rebase-merge directory`, naming a path inside attic's private store. A snapshot hook driving that loop discards one side of a conflict silently, once per run.

To find one you have not opened lately, `attic doctor` reports it across every overlay on the machine and exits non-zero:

```text
STATUS  FP            LABEL                      DETAIL
wedged  c7ba88d2b618  ravinald/netbox-flow-view  stopped mid-rebase with 3 commit(s) on no other ref — resolve in /Users/you/git/netbox-flow-view, `--quit` to keep them
```

doctor never resolves one under `--fix`: choosing between `--continue` and `--abort` decides which side of a conflict survives.

## `git push` is rejected: "non-fast-forward"

Two machines pushed to the same `repo/<fp>` branch without one pulling first. Standard git: `attic pull`, resolve any conflicts, `attic push` again.

## `attic add` warned that a path is already registered

Two different intents share one verb's shape, so `add` distinguishes them rather than guessing:

- **`attic add <path>`** registers a path: it writes the path into the host `.gitignore` block and stages it. This is a one-time act per path.
- **`attic stage [<path>...]`** re-stages what the block already registers, and never touches the block. This is how new files under a managed directory reach the overlay index.

`attic add docs-internal` on an already-registered directory still stages it, so nothing breaks; the warning points at `attic stage` because that is the verb that says what you meant. `attic add docs-internal/new-file.md` warns and leaves the block **unchanged**: the `docs-internal` entry already covers the file, so a new line would ignore nothing further while making the block misreport the granularity the overlay is managed at.

Registering a file beneath an already-registered directory is the one case worth checking yourself. If the block genuinely should track that file rather than its parent directory, the parent entry is the thing to remove (`attic rm`), not the file to add.

## An overlay path keeps showing up staged in the host repo

A `.gitignore` rule can't untrack a path already in the host index or stop a `git add -f`, so once an overlay path lands in the host index (a stray force-add, a headless script, a pre-`attic` commit) it sticks and every commit trips the guard. Run `attic eject` from the host repo: it evicts every managed path from the host index while leaving the working-tree files and overlay history untouched. `attic eject --check` reports without changing anything; wire it into a pre-commit hook to catch the regression early.

## I committed an overlay path to the host repo by accident

Remove it from the host upstream (`git rm --cached path && git commit && git push`), then `attic eject` to keep the index clean going forward. The marker block in `.gitignore` plus `attic add`'s host-index eviction exist to prevent this, so check the block is intact and contains the path.

## `attic status` says clean, but I know I added a file

The host `.gitignore` hides overlay-owned paths from git and outranks the overlay's own `info/exclude`, so git will never volunteer a _new_ file under `notes/`, not in `git status` and not with `-uall`. `attic status` asks for those by name and prints them under **"Untracked overlay files"** below git's own output. If that section lists your file, it exists but was never staged. Use `attic stage`, not `attic add`: the file sits under a directory the block already registers, and `add` would append a redundant rule naming it.

If you piped the command (`--porcelain`, `-s`, `-z`), the header is dropped but the files still arrive, as `?? <path>` on git's own stream (`? <path>` under `--porcelain=v2`, NUL-terminated under `-z`). So `attic status --porcelain | wc -l` is a usable dirtiness check. Pass `--ignored` and attic stays quiet, because git's `!!` lines already cover the same files.

## `attic commit` says "nothing staged for commit"

Edits to already-tracked overlay files aren't staged automatically. Use `attic commit -a`. New files need staging first: `attic stage` for anything under a path the overlay already manages, `attic add <path>` only to register a path the block doesn't cover yet. The error lists any it can see.

## Every attic command says "no overlay for &lt;path&gt;" but the overlay existed yesterday

You rewrote the host repo's history. attic keys overlay storage by the host's root commit, so `git filter-repo`, `filter-branch`, a squashed or grafted root, or an amended root commit all move the key and orphan the overlay. The history is not lost: it is filed under the old fingerprint, on disk and on the remote.

Run **`attic rekey`** inside the host repo. It names both fingerprints, moves the storage dir, renames the `repo/<fp>` branch, rewrites the branch config and fetch refspec, and updates `meta.toml`. `--dry-run` prints the plan first. Then `attic push` to publish the new branch; the old one stays on the mono remote as a fallback.

Do **not** run `attic init` here. It would start an empty overlay beside the populated one and leave that history unreachable. The error says so, and refuses to offer init when it can see orphaned storage.

`attic doctor` reports orphans across every overlay on the machine, which is how you find one in a repo you haven't opened lately.

Two things to know before rewriting a host repo's history, because both bite after the fact rather than during:

- A `git reset --hard` onto the rewritten history **deletes every overlay file the host index also tracked**, since those paths are in the old index and absent from the new HEAD. Untracked overlay files survive. Recover with `attic rekey`, then `attic exec -- checkout -- <path>`.
- If a snapshot hook runs while the overlay is orphaned it fails silently and records nothing, so a change made in that window is only on disk. Commit it after re-keying.

## `attic labels push` didn't rename my overlay

`push` is **contribute-only**: it fills in fingerprints the shared map has never seen and never overwrites an existing entry. That's deliberate: it's what stops one machine's push clobbering a name someone curated. To rename an existing entry, use `attic labels edit`, the only writer allowed to overwrite.

If the name is only wrong on _this_ machine, you want a local override instead: `attic label set <name>` (never pushed).

## `attic doctor` reports an overlay as `overridden` and won't fix it

The overlay has a local override in `~/.config/attic/overrides.toml`, and doctor honors your local choice over the origin slug. To hand it back to doctor's auto reconciliation, clear the override with `attic label reset` (it lists what it would clear unless you pass `--force`).

## `attic labels` says "this machine has more than one mono remote"

The `labels` commands resolve the remote from the current overlay, then fall back to the machine's sole mono remote. With two or more, that fallback is ambiguous, so name it: `attic labels push --remote git@github.com:you/attic-overlays.git`. Or run the command from inside a host repo whose overlay already points at the remote you mean. `attic clone --mono` with no URL uses the same fallback and reports the same thing.

Spelling is not ambiguity. Overlays record the URL as it was typed, so one remote accumulates several forms (`…/attic-overlays`, `…/attic-overlays/`, `…/attic-overlays.git`); those count as one remote, and the form most overlays already use is the one attic hands git. Transport is a real difference: `git@github.com:you/attic-overlays.git` and `https://github.com/you/attic-overlays` name one repo but authenticate differently, so a machine holding both has two mono remotes and naming one is the answer.

## `attic add` warns that a `.gitignore` rule is now redundant

You hand-wrote an ignore rule for that path before adopting attic, so the managed block now shadows it. Harmless, but two sources of truth. Absorb the old rule:

```sh
attic config set gitignore.on_duplicate manage    # this repo
attic add notes/                                  # deletes the redundant outside rule
```

`manage` only touches slash-equivalent, glob-free rules: a real pattern like `docs-*` is never second-guessed, and lines inside another tool's markers are left alone. Silence the notice without changing anything with `off`.

## I want to remove an overlay entirely

```sh
attic deinit          # from inside the host repo
```

This deletes the bare overlay and its meta, and strips attic's block from the host `.gitignore`. Work-tree files stay on disk: `deinit` forgets how to track them, it doesn't delete them. It refuses when the overlay holds commits not on its remote; `attic push` first, or `--force` if you mean to drop them.

The remote side is untouched. For a mono remote, delete the branch yourself:

```sh
attic exec -- push origin --delete repo/$(attic where --fp)   # BEFORE deinit: needs the overlay
```

## I want to use a different default branch name (not "main")

For per-host mode, set it manually after `init`:

```sh
attic exec -- symbolic-ref HEAD refs/heads/master   # for example
```

For mono mode, the branch name is derived from the fingerprint and shouldn't change.

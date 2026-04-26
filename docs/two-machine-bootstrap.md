# Bootstrapping an overlay when both machines already have the directory

You wrote `attic`. You haven't used it yet on `ravinald/wifimgr`. The host repo is cloned on **work** and **home**, both already have a populated `docs-internal/`, and the two copies have diverged. Goal: create the overlay, fold both copies into one shared history, push, end up with a sane merged tree on both machines.

## What not to do

- **`attic init` on both machines and then push from each.** Same `host/<fp>` branch, two unrelated histories — second push gets rejected, and you have to merge with `--allow-unrelated-histories` anyway. Plan for the merge from the start.
- **`attic clone --mono` on the second machine without moving its local copy aside first.** Clone refuses to clobber existing files. `--force` overwrites home's edits and you lose work. Don't.
- **Skipping the fingerprint check.** Both machines must agree on the host repo's root commit. If they don't, you've got a different problem to fix first (rebased root, fork, etc.) — see `troubleshooting.md`.

```sh
# Confirm fingerprint matches on BOTH machines before anything else:
attic where --fp        # run on work
attic where --fp        # run on home — must match
```

## Recommended path: git-native merge of unrelated histories

This treats both machines as equals, lets git produce conflict markers for files that diverged, and ends with a real merge commit recording both parents.

### 1. On **work**: bootstrap and push

```sh
cd ~/git/wifimgr
attic init --mono-remote git@github.com:ravinald/attic-overlays.git
attic add docs-internal/
attic commit -m "wifimgr: docs-internal (work)"
attic push
```

Work's content is now on `origin/host/<fp>`.

### 2. On **home**: initialise locally and commit home's copy

`docs-internal/` is already populated with home's content — don't move it.

```sh
cd ~/git/wifimgr
attic init --mono-remote git@github.com:ravinald/attic-overlays.git
attic add docs-internal/
attic commit -m "wifimgr: docs-internal (home)"
```

Two unrelated histories now exist: work's on the remote, home's local-only.

### 3. Fetch work and merge

```sh
attic fetch
attic exec -- merge --allow-unrelated-histories \
  origin/host/$(attic where --fp) -m "merge home + work docs-internal"
```

Files present on only one side: auto-merged.
Files present on both with different content: conflict markers in the work tree.

Resolve conflicts in your editor, then:

```sh
attic add docs-internal/<conflicted-paths>
attic commit -m "resolve home/work conflicts"
```

### 4. Push the merge

```sh
attic push
```

The merge commit has work's commit as a parent, so the push fast-forwards.

### 5. Back on **work**: pull

```sh
cd ~/git/wifimgr
attic pull
```

Both machines now share one linear-from-here history.

## Fallback: stash-aside, clone, merge with a visual diff tool

Skip git's conflict markers if you'd rather drive the merge through `meld` / `kdiff3` / `vimdiff`.

```sh
# on home, after step 1 has happened on work
cd ~/git/wifimgr
mv docs-internal docs-internal.home          # park home's copy
attic clone --mono git@github.com:ravinald/attic-overlays.git
meld docs-internal docs-internal.home        # or kdiff3, vimdiff -d, etc.
attic add docs-internal/                     # picks up new files added during merge
attic commit -m "merge home edits into wifimgr docs-internal"
attic push
rm -rf docs-internal.home
```

Same end state, manual diff tool instead of git's resolver. Use this when the divergence is large enough that conflict markers would be more pain than help.

## Things to know

- **`.gitignore` block lands on the host work tree.** `attic add` writes the marker block into `wifimgr/.gitignore`. That's a host-repo change. Commit it upstream (`git add .gitignore && git commit -m "ignore docs-internal (attic overlay)"`) so collaborators don't see a perpetually dirty `.gitignore`.
- **No three-way merge base.** Unrelated histories share no ancestor, so git falls back to a two-way diff on file content. Expect more conflict markers than a routine merge — review every hunk.
- **Mono branch name is the fingerprint.** That's why both machines push to the same `host/<fp>` and why the fingerprints MUST match in step 0. The branch is the contract.
- **Per-host remote variant.** If you used `--remote` or `--gh-private` instead of `--mono-remote`, the flow is identical except the branch is `main` and the merge target is `origin/main`. Substitute accordingly.

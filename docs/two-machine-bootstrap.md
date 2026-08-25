# Bootstrapping an overlay when both machines already have the directory

You want to adopt `attic` on a repo you've been working in for a while. The host repo is cloned on **work** and **home**, both already have a populated `notes/`, and the two copies have diverged. Goal: create the overlay, fold both copies into one shared history, push, end up with a sane merged tree on both machines.

## What not to do

- **`attic init` on both machines and then push from each.** Same `repo/<fp>` branch, two unrelated histories: the second push gets rejected, and you have to merge with `--allow-unrelated-histories` anyway. Plan for the merge from the start.
- **`attic clone --mono` on the second machine without moving its local copy aside first.** Clone refuses to clobber existing files, and `--force` overwrites home's edits and loses work. Don't. (`--force` stops at paths the host repo tracks, but the copy at issue here is untracked by construction, so that guard does not cover you.)
- **Skipping the fingerprint check.** Both machines must agree on the host repo's root commit. If they don't, you've got a different problem to fix first (a rewritten root, a fork). See [troubleshooting.md](troubleshooting.md).

```sh
# Confirm fingerprint matches on BOTH machines before anything else:
attic where --fp        # run on work
attic where --fp        # run on home — must match
```

## Recommended path: git-native merge of unrelated histories

This treats both machines as equals, lets git produce conflict markers for files that diverged, and ends with a real merge commit recording both parents.

### 1. On **work**: bootstrap and push

```sh
cd ~/git/myproject
attic init --mono-remote git@github.com:you/attic-overlays.git
attic add notes/
attic commit -m "myproject: notes (work)"
attic push
```

Work's content is now on `origin/repo/<fp>`.

### 2. On **home**: initialize locally and commit home's copy

`notes/` is already populated with home's content — don't move it.

```sh
cd ~/git/myproject
attic init --mono-remote git@github.com:you/attic-overlays.git
attic add notes/
attic commit -m "myproject: notes (home)"
```

Two unrelated histories now exist: work's on the remote, home's local-only.

### 3. Fetch work and merge

```sh
attic fetch
attic exec -- merge --allow-unrelated-histories \
  origin/repo/$(attic where --fp) -m "merge home + work notes"
```

Files present on only one side: auto-merged.
Files present on both with different content: conflict markers in the work tree.

Resolve conflicts in your editor, then finish the merge through `attic exec`. `attic add`, `attic stage` and `attic commit` all refuse while a merge is open, because staging over an unresolved conflict silently discards one side of it:

```sh
attic exec -- add --force -- notes/<conflicted-paths>
attic exec -- merge --continue
```

Add `-c core.editor=true` before `merge` if `$EDITOR` opening on the merge message is unwelcome; git then takes its own default message.

### 4. Push the merge

```sh
attic push
```

The merge commit has work's commit as a parent, so the push fast-forwards.

### 5. Back on **work**: pull

```sh
cd ~/git/myproject
attic pull
```

Both machines now share one linear-from-here history.

## Fallback: stash-aside, clone, merge with a visual diff tool

Skip git's conflict markers if you'd rather drive the merge through `meld` / `kdiff3` / `vimdiff`.

```sh
# on home, after step 1 has happened on work
cd ~/git/myproject
mv notes notes.home                          # park home's copy
attic clone --mono git@github.com:you/attic-overlays.git
meld notes notes.home                        # or kdiff3, vimdiff -d, etc.
attic stage notes/                           # picks up new files added during merge
attic commit -m "merge home edits into myproject notes"
attic push
rm -rf notes.home
```

Same end state, manual diff tool instead of git's resolver. Use this when the divergence is large enough that conflict markers would be more pain than help.

## Things to know

- **The `.gitignore` block lands on the host work tree.** `attic add` writes the marker block into `myproject/.gitignore`, which is a host-repo change. Commit it upstream (`git add .gitignore && git commit -m "ignore notes/ (attic overlay)"`) so collaborators don't see a perpetually dirty `.gitignore`.
- **No three-way merge base.** Unrelated histories share no ancestor, so git falls back to a two-way diff on file content. Expect more conflict markers than a routine merge, and review every hunk.
- **The mono branch name is the fingerprint.** That's why both machines push to the same `repo/<fp>`, and why the fingerprints MUST match before you start. The branch is the contract.
- **Per-host remote variant.** If you used `--remote` or `--gh-private` instead of `--mono-remote`, the flow is identical except the branch is `main` and the merge target is `origin/main`. Substitute accordingly.

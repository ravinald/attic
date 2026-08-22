# Changelog

All notable changes to `attic`.

## [v0.2.0] — 2026-08-22

### Added
- `attic rekey` re-points an overlay orphaned by a host history rewrite. Overlay storage is keyed by the host repo's root commit, so `git filter-repo`, `filter-branch`, a squashed or grafted root, or an amended root commit moves the key and every command then reports `no overlay for <path>` while the history sits intact on disk and on the remote. rekey moves the storage dir, renames the `repo/<fp>` branch, rewrites the branch config and fetch refspec, and updates `meta.toml`; `--dry-run` prints the plan. Overlay history is never rewritten, only re-filed. The previous guidance was a documented manual `mv` that omitted the branch rename and the refspec, so it left mono overlays half-wired.
- `attic stage [<path>...]` re-stages new and modified files under paths the overlay already manages, without touching the host `.gitignore` block. With no arguments it stages every managed entry, which is what a snapshot hook wants; with arguments it refuses paths the block doesn't cover.
- `status.ignore` — glob patterns hidden from the untracked-overlay-files list in `attic status` and `attic commit`. Every file under overlay scope is ignored by construction, so no `.gitignore` rule can trim that list; this filters after the `ls-files` query. Basename patterns (`.DS_Store`), directory patterns (`scratch/`), or full host-relative paths (`notes/*.tmp`); `**/` is accepted as a synonym for the basename form. No defaults ship — a filter that hides a file the overlay should have adopted is worse than the noise it removes. Layers union rather than override (`ATTIC_STATUS_IGNORE` env + per-repo + global), so a per-repo pattern never silences the global list. A malformed pattern warns on stderr and hides nothing.
- `attic sync [--strategy=rebase|merge]` — the steady-state multi-machine loop: fetch, integrate, push. Rebase by default to keep `repo/<fp>` linear; `merge` is fast-forward-only. Refuses a dirty index or edits to overlay-tracked files, and works on the first sync after `init` when no upstream exists yet.
- `attic list [--fetch] [--wide] [--json]` — one row per overlay on the machine, with label, fingerprint, host root, branch, and sync state. Sync state reads already-fetched refs unless `--fetch` is passed.
- `attic eject [--check]` — evict attic-managed paths from the **host** index, leaving working-tree files and overlay history untouched. `--check` reports without changing and exits non-zero when a managed path is staged as a host addition, the form a pre-commit guard calls.
- `attic deinit [--force]` — undo `init`/`clone`: remove the local bare overlay, its meta, and attic's `.gitignore` block. Work-tree files stay. Refuses when the overlay holds commits not on its remote unless `--force`.
- `attic config get|set|list [--global]` — read/write settings per-repo (the overlay's `meta.toml`) or globally (`~/.config/attic/config.toml`). First key: `gitignore.on_duplicate`.
- `attic add --on-duplicate off|warn|manage` — handle a path already ignored by a rule *outside* the managed block. `warn` (default) names the redundant rule; `manage` deletes it so the block is the single source; `off` stays silent. Only slash-equivalent, glob-free rules qualify for `manage`, and lines inside another tool's markers are never touched. Precedence: flag › `ATTIC_GITIGNORE_ON_DUPLICATE` › per-repo › global › `warn`.
- Labels, in two layers so no machine owns an overlay: a shared map (`labels.toml` on the mono remote's `_attic/labels` branch) and per-machine overrides (`~/.config/attic/overrides.toml`) that never leave the machine.
  - `attic label get` resolves override → shared/auto name → repo basename. `attic label set` writes a local override (`--unset` clears it); `attic label reset [--force]` clears every override on the machine.
  - `attic labels edit` opens the whole map in `$EDITOR` (visudo-style), then validates, regenerates the README, and pushes on save — the only way to rename an existing entry, including a "foreign" overlay whose host repo isn't on this machine.
  - `attic labels push` is contribute-only: it fills in fingerprints the map hasn't seen and never overwrites a curated name. `attic labels pull` caches the map's names locally for display.
  - `attic labels edit|push|pull` run from anywhere — they fall back to the machine's sole mono remote and take `--remote <url>` to disambiguate.
  - Labels auto-derive from the host repo's origin as `owner/repo` at `init`/`clone`. Provenance is tracked (`label_source = origin|manual`) so a hand-set label is never silently overwritten.
- `attic doctor [--fix] [--force] [--push]` — audit every overlay's label against its origin's `owner/repo` slug. Reports by default and exits non-zero on fixable drift (hook-friendly); `--fix` rewrites auto-derived labels and refreshes moved origins in local meta; `--force` also adopts the slug over a hand-set label; `--push` publishes the corrected map. Plain `--fix` stays local, so hooks and offline runs never touch the network. An overlay with a local override is reported as `overridden` and left alone.
- `attic init --mono-remote` bootstraps a fresh mono repo: seeds the `_attic/labels` branch (`labels.toml` plus a browsable README table linking each label to its `repo/<fp>` branch and commit log) and sets the repo default branch to `_attic/labels` via `gh`, so the map is the landing page.
- The overlay writes `/*` into a marker block in its `info/exclude`. Without it the overlay's work tree — the whole host repo — reads as entirely untracked. Existing overlays are healed on the next command.
- `attic status` lists untracked files under overlay-owned paths in a separate section. The host `.gitignore` outranks `info/exclude`, so git never reported a new file under `notes/` at all. Suppressed under `--porcelain`/`-s`/`-z` so a parsing caller sees only git's stream.

### Changed
- `attic add` now distinguishes registering a path from re-staging one. Re-adding an already-registered path still stages it and points at `attic stage`; naming a path already covered by a broader entry warns and leaves the block **unchanged**, where before it appended a rule that ignored nothing further and made the block misreport the granularity the overlay is managed at. `Block.Add` dedupes by exact string, which is why a file beneath a registered directory used to slip through.
- `attic doctor` reports overlays orphaned by a history rewrite, so one in a repo you haven't opened lately still surfaces. Reported only, never re-keyed under `--fix`: moving a storage directory wants the operator present.
- `attic commit`'s "nothing staged" error points at `attic stage` instead of `attic add`. Everything it can report sits under an already-registered path, so add was always the redundant-rule-producing verb there.
- Overlay branches on a mono remote are named `repo/<fp>` rather than `host/<fp>` — the fingerprint identifies the git repo, not a host.
- `attic commit` without `-m` commits with a timestamped `attic snapshot <UTC>` message instead of opening an editor that aborts on an empty message.
- `attic clone --mono` may omit the remote URL — it defaults to this machine's sole mono remote, matching the `labels` commands. Pass the URL explicitly when the machine tracks more than one.
- A label may contain `/` (an `owner/repo` slug). Leading and trailing `/`, `//`, and `..` segments stay rejected.
- `attic commit` with nothing staged names what it can see and points at `attic add` / `attic commit -a`, instead of surfacing a bare `gitwrap: git commit … failed: exit status 1`.

### Fixed
- `attic sync`, `commit`, `stage`, `add`, `pull` and `push` refuse an overlay stopped part-way through a rebase, merge, cherry-pick, revert, `am` or bisect. `git status --porcelain` reports none of those (the banner is in the long format only), so `sync`'s clean-index check read a stopped rebase as a clean repo and started a second one, dying on git's `fatal: It seems that there is already a rebase-merge directory` and an `rm -fr` hint naming a path inside attic's private store. Worse, `stage` marked the conflicted path resolved with whatever sat in the work tree and `commit` baked it in, so a snapshot hook looping on that pair discarded one side of a conflict per run while every surface reported a clean tree. Found in the field on two overlays whose dated changelogs both machines had appended to; one had lost 496 lines of an append-only file locally. `attic fetch`, `log`, `diff` and `exec` stay open — they are how you diagnose it, and `attic exec rebase --continue|--abort` is the recovery the refusal names.
- The wedge refusal and `attic doctor` count commits reachable only from HEAD and lead with `attic exec rebase --quit` when there are any. `--abort` resets the branch to where the operation started, so on an overlay a snapshot hook kept committing over it, `--abort` destroys that work; `--quit` closes the state and leaves the tip alone (HEAD detached, branch re-pointed by hand). The first cut counted against `REBASE_HEAD`, which over-reports by including commits already replayed onto the new base.
- `attic doctor` reports such an overlay as `wedged` and exits non-zero, so one in a repo you haven't opened lately still surfaces. Never resolved under `--fix`: choosing between `--continue` and `--abort` decides which side of a conflict survives.
- `attic add`/`rm`/`stage` rejected a path that doesn't exist yet as "outside host repo". The host root is canonicalised at detection, but `EvalSymlinks` can only resolve a path that exists, so a not-yet-created path kept its uncanonical form; on macOS the root reads `/private/var/…` while the cwd reads `/var/…`, and `filepath.Rel` between the two forms returns `../…`, tripping the outside-repo guard on a path plainly inside the repo. The nearest existing ancestor is now resolved instead of the whole path. Needed both a missing path and a symlinked prefix to trigger, which is why a repo under `~/git` never showed it and a `mktemp -d` sandbox showed it immediately.
- Mono overlays fetched the whole store. `attic clone --mono` single-branch-cloned correctly, then immediately widened `remote.origin.fetch` to `+refs/heads/*:…` and fetched again, pulling every other project's overlay history into this one's bare; `attic init --mono-remote` inherited the same wildcard from `git remote add`. A 31 KiB restore against an 18-overlay store cost 45 MiB, repeated per overlay on the machine. Both paths now pin the refspec to `repo/<fp>` alone, and `attic sync` re-pins an overlay still wired wide. Per-host overlays are unaffected — the wildcard is correct there.
- Silent data-loss footgun in `labels edit`/`push`: `openLabelsWorktree` swallowed an `ls-remote` error and read an unreachable remote as an absent branch, so a transient network blip could publish a machine-local map and prune every entry for an overlay not cloned locally. It now aborts on that error.
- `attic clone --mono` left the overlay with no upstream — `git clone --bare` sets no fetch refspec or remote-tracking refs, so the overlay reported `no-upstream` and could neither compute sync state nor pull. Clone now restores the standard refspec and sets the branch's upstream, matching `attic init`.
- `attic init` publishes the label map on every mono overlay, not only the first one on a remote.
- `on_duplicate=warn` speaks only for paths a run newly adopts, so re-running `attic add notes/` to stage new files under an already-managed directory stays quiet. `manage` still scans every path, so switching to it later absorbs a rule an earlier `off`/`warn` run left behind.

## [v0.1.0] — 2026-04-26

Initial public release.

### Features

- `attic init [--remote URL | --mono-remote URL | --gh-private]` — create a per-host bare overlay under `$XDG_DATA_HOME/attic/repos/<fp>/`. Host identity is the host repo's root commit SHA (12-char prefix). With no flag the overlay is local-only; a remote can be wired later. Mutually-exclusive remote modes:
  - per-host (`--remote`): one git repo per host, branch `main`
  - mono (`--mono-remote`): one shared git repo, branch `host/<fp>` per host. `push.default=current` + `push.autoSetupRemote=true` configured automatically.
  - `--gh-private`: per-host mode with `gh repo create --private` integration.
- `attic clone <remote> [--mono]` — restore an overlay on a fresh machine. Mono variant single-branch-clones `host/<fp>` only and pre-flights with `git ls-remote` for a useful error if the branch is missing. Refuses to clobber existing files unless `--force`.
- `attic add <path>...` / `attic rm <path>... [--delete]` — manage paths in the marker-delimited `# BEGIN attic` / `# END attic` block of the host repo's `.gitignore`. Atomic write via tmp + rename. `add` uses `git add --force` (the force is intentional — gitignored upstream and tracked in the overlay is the design).
- `attic commit`, `status`, `push`, `pull`, `fetch`, `log`, `diff`, `ls` — pass-throughs to git in the overlay context.
- `attic where [--fp]` — print bare path, fingerprint, branch, remote.
- `attic exec -- <git-args>` — escape hatch.
- `attic version` — version, commit, build date (baked via `-ldflags`).

### Quality

- `golangci-lint` (v2 schema) clean.
- `govulncheck` clean.
- Unit tests for the `ignore.Block` splice/idempotence logic.

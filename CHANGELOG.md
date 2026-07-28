# Changelog

All notable changes to `attic`.

## [v0.1.0] — unreleased

Initial public release.

### Features

- `attic init [--remote URL | --mono-remote URL | --gh-private | --no-flag]` — create a per-host bare overlay under `$XDG_DATA_HOME/attic/repos/<fp>/`. Host identity is the host repo's root commit SHA (12-char prefix). Mutually-exclusive remote modes:
  - per-host (`--remote`): one git repo per host, branch `main`
  - mono (`--mono-remote`): one shared git repo, branch `host/<fp>` per host. `push.default=current` + `push.autoSetupRemote=true` configured automatically.
  - `--gh-private`: per-host mode with `gh repo create --private` integration.
- `attic clone <remote> [--mono]` — restore an overlay on a fresh machine. Mono variant single-branch-clones `host/<fp>` only and pre-flights with `git ls-remote` for a useful error if the branch is missing. Refuses to clobber existing files unless `--force`.
- `attic add <path>...` / `attic rm <path>... [--delete]` — manage paths in the marker-delimited `# BEGIN attic` / `# END attic` block of the host repo's `.gitignore`. Atomic write via tmp + rename. `add` uses `git add --force` (the force is intentional — gitignored upstream and tracked in the overlay is the design).
- `attic add --on-duplicate off|warn|manage` — handle a path already ignored by a rule *outside* the block. `warn` (default) notes the redundant rule; `manage` deletes it so the block is the single source; `off` stays silent. Only slash-equivalent, glob-free rules qualify for `manage`. Precedence: flag › `ATTIC_GITIGNORE_ON_DUPLICATE` env › per-repo › global › `warn`. `warn` fires only for paths a run newly adopts, so re-adding an already-managed directory to stage new files under it is silent; `manage` keeps scanning every path so a later switch still absorbs an old rule.
- `attic config get|set|list [--global]` — read/write settings, per-repo (overlay `meta.toml`) or global (`~/.config/attic/config.toml`). First key: `gitignore.on_duplicate`.
- `attic commit`, `status`, `push`, `pull`, `fetch`, `log`, `diff`, `ls` — pass-throughs to git in the overlay context.
- `attic where [--fp]` — print bare path, fingerprint, branch, remote.
- `attic exec -- <git-args>` — escape hatch.
- `attic version` — version, commit, build date (baked via `-ldflags`).
- Labels now auto-derive from the host repo's origin as `owner/repo` at `init`/`clone` — no manual `attic label set` for the common case. Provenance is tracked (`label_source = origin|manual`) so a hand-set label is never silently overwritten. `attic label set` still accepts any name, and a label may now contain `/` (an `owner/repo` slug); leading/trailing `/`, `//`, and `..` segments stay rejected.
- `attic doctor [--fix] [--force]` — sweeps every overlay on the machine and reports labels that drifted from their origin slug (or origins that moved). Reports by default and exits non-zero on fixable drift (hook-friendly); `--fix` rewrites auto-derived labels and refreshes moved origins; `--force` also adopts the origin slug over a hand-set label. Touches only local meta — run `attic labels push` to publish.
- `attic labels push` now also writes a `README.md` on the `_attic/labels` branch: a markdown table linking each `owner/repo` label to its `host/<fp>` branch, so the mono remote is browsable on the web without decoding `labels.toml`.
- `attic deinit [--force]` — undo `init`/`clone`: removes the local bare overlay, its meta, and attic's `.gitignore` block. Work-tree files are left in place. Refuses when the overlay holds commits not on its remote unless `--force`.
- `attic commit` without `-m` now commits with a timestamped `attic snapshot <UTC>` message instead of dropping into an editor that aborts on an empty message.
- `attic doctor --fix --push` publishes the corrected map to each affected mono remote in one shot (chains `attic labels push`). Plain `--fix` stays local, so hooks and offline runs never touch the network.
- `attic labels edit` — edit the whole map in `$EDITOR` (visudo-style): pulls the current names, opens a `<fingerprint>  <label>` table, then validates, regenerates the README, and pushes on save. The only way to rename a "foreign" overlay whose host repo isn't on this machine.
- `attic init --mono-remote` now bootstraps a fresh mono repo: seeds the `_attic/labels` branch (labels.toml + README map) and sets the repo default branch to `_attic/labels` (best-effort via `gh`), so the map is the landing page without manual setup.
- Labels split into two layers so no machine "owns" an overlay: the shared map on `_attic/labels` (canonical, edited via `attic labels edit`) and per-machine overrides in `~/.config/attic/overrides.toml` that never leave the machine. `attic label set` now writes a local override (`--unset` clears it); `attic label get`/`list` resolve override → shared/auto name → basename. `attic labels push` is contribute-only — it fills in overlays the map hasn't seen, never overwriting a curated name.
- `attic doctor` honours local overrides — an overridden overlay is reported as `overridden` and left alone, never flagged as drift. `attic label reset [--force]` clears every local override on the machine (lists them without `--force`) to hand overlays back to doctor's auto reconciliation.
- Overlay branches on a mono remote are now named `repo/<fp>` instead of `host/<fp>` — the fingerprint identifies the git repo, not a host. The browsable map gains a **History** column linking to each branch's commit log.
- `attic labels edit/push/pull` now work from anywhere, not just inside a mono overlay's repo — they fall back to the machine's sole mono remote, and take a `--remote <url>` to disambiguate when there's more than one.
- Fixed a silent data-loss footgun: `openLabelsWorktree` swallowed an `ls-remote` error and treated an unreachable remote as an absent branch, so a transient network blip during `labels edit`/`push` could publish a localhost-only map and prune every entry for an overlay not cloned locally. It now aborts on that error instead.
- `attic clone --mono` may now omit the remote URL — it defaults to this machine's sole mono remote, matching the `labels` commands. Pass the URL explicitly when the machine has more than one mono remote.
- Fixed `attic clone --mono` leaving the overlay with no upstream: `git clone --bare` sets no fetch refspec or remote-tracking refs, so the overlay reported `no-upstream` and couldn't compute sync state or pull. Clone now restores the standard refspec and sets the branch's upstream, matching what `attic init` produces.
- Fixed the overlay treating the entire host repo as untracked. The overlay's work tree is the whole host repo, so `attic status` listed every host file and `attic commit` died with git's "nothing added to commit but untracked files present" followed by the host's top level. attic now writes `/*` into a marker block in the overlay's `info/exclude`; existing overlays are healed on the next command.
- `attic status` now lists untracked files under overlay-owned paths. The host `.gitignore` outranks `info/exclude`, so git never reported a new file under `docs-internal/` at all — it stayed invisible until someone noticed it had never been pushed.
- `attic commit` with nothing staged now names what it can see and points at `attic add` / `attic commit -a`, instead of surfacing a bare `gitwrap: git commit … failed: exit status 1`.

### Quality

- `golangci-lint` (v2 schema) clean.
- `govulncheck` clean.
- Unit tests for the `ignore.Block` splice/idempotence logic.

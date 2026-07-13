# Mono remote guardrails

A mono remote (`attic init --mono-remote <url>`) is an **overlay store**, not a
collaborative repo. Every project is a branch named `host/<fingerprint>`, and each of
those branches is an **independent orphan history** — its own root commit, its own tree
rooted at a different repo. Attic pushes to them directly with `attic push` / `attic sync`.

There is no legitimate pull request on this remote. Merge one `host/<fp>` branch into
another (or into any base branch) and you splice unrelated trees together — the next
`attic clone` or `attic sync` for that fingerprint checks out files that don't belong to
the repo. GitHub has no switch to disable the Pull Requests feature, and as repo admin you
can bypass most protections, so the goal here is **speed bumps against your own reflexes**,
not a wall against an adversary.

The hard constraint: attic pushes *directly* to branches, so any guard that blocks direct
ref updates breaks attic. Guards may only block **PR merges**.

## Layer 1 — auto-close every PR (zero risk to attic)

Drop [`examples/mono-remote/.github/`](../examples/mono-remote/.github/) into the root of
your mono remote:

- `workflows/block-overlay-prs.yml` — closes any PR the moment it opens, with a comment
  pointing back to `attic push` / `attic sync`. Pushes aren't PRs, so attic never notices.
- `PULL_REQUEST_TEMPLATE.md` — a loud **DO NOT MERGE** banner for the split second a PR
  is open.

```sh
cd <clone-of-your-mono-remote>
mkdir -p .github/workflows
cp -r /path/to/attic/examples/mono-remote/.github/. .github/
git add .github && git commit -m "guard: block overlay PRs" && git push
```

This covers ~90% of the risk and cannot interfere with attic.

## Layer 2 — ruleset that reddens the merge button

A repository **ruleset** targeting **All branches** with a single **Require status checks**
rule naming a check that no workflow ever reports (e.g. `overlays-never-merge`), and an
**empty bypass list**. No PR ever goes green, so the merge button stays blocked — even for
you. "Require status checks" gates merges, not direct pushes, so `attic push` keeps working.

Do **not** add "Require a pull request before merging" — that rule blocks direct pushes and
breaks attic.

The empty bypass list is the point: the admin (you) is exactly who this blocks.

> Provisioning this ruleset from attic (`attic guard apply`) is designed but unbuilt —
> see the internal design note. Until then, apply it once by hand in
> **Settings → Rules → Rulesets**, or via `gh api`.

After applying, verify attic still works:

```sh
cd ~/git/<any-tracked-repo>
attic status && echo "test" >> docs-internal/scratch && attic add docs-internal/scratch
attic commit -m "guard smoke test" && attic push   # must succeed
```

If the push is rejected, the ruleset is gating direct updates — remove "Require a pull
request before merging" (or the whole ruleset) and rely on Layer 1.

## Layer 3 — shrink the surface

- **Default branch = an inert orphan `main`** holding only a `README.md` that says
  "attic overlay store — branches are `host/<fp>`, never PR." GitHub's *Compare & pull
  request* banner targets the default branch; point it at a dead end.
- **Disable Issues, Wikis, Projects, Discussions** in repo settings.
- Turn off squash and rebase merges (GitHub forces at least one method on), leaving only
  merge commits — fewer buttons to misclick.
</content>

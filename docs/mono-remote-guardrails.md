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

This is the enforcement — the only mechanism that blocks merges without touching `attic
push`. The surface-reduction settings below are hygiene on top of it.

## Why not a branch-protection ruleset?

A ruleset that "requires a status check that never passes" looks like it would redden the
merge button. It doesn't work for attic, for two independent reasons:

- **Rulesets and branch protection need GitHub Pro on private repos.** The mono remote is
  private by design; on a Free personal account, `POST /rulesets` returns
  `403 Upgrade to GitHub Pro or make this repository public`.
- **A ruleset gates every ref update, not just merges.** With a `required_status_checks`
  rule active on all branches, a plain `attic push` to `host/<fp>` is *rejected*
  (`GH013 ... Required status check ... push declined`). GitHub can't distinguish a PR
  merge from a direct push — both are ref updates to the same branch. So any ruleset
  strong enough to block the merge also bricks `attic push`.

Only the **Layer 1 Action** distinguishes them: it keys on the `pull_request` event, not on
the ref update, so it closes PRs while leaving pushes untouched. That's why the Action —
not a ruleset — is the enforcement here. (Verified empirically; see the internal design
note for the spike.)

## Layer 2 — shrink the surface

- **Default branch = an inert orphan `main`** holding only a `README.md` that says
  "attic overlay store — branches are `host/<fp>`, never PR." GitHub's *Compare & pull
  request* banner targets the default branch; point it at a dead end.
- **Disable Issues, Wikis, Projects, Discussions** in repo settings.
- Turn off squash and rebase merges (GitHub forces at least one method on), leaving only
  merge commits — fewer buttons to misclick.
</content>

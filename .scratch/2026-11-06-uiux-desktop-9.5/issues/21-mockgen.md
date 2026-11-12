# Generate mocks from templ

Created: 2026-11-06
Category: enhancement
Status: wontfix
Blocked by: 20

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/mockgen` | `.worktrees/tech+mockgen` | `tech(mockups): render shipped pages from fixtures` | mocks | L | Mocks → 9.5, fidelity → 9.5 |

## Problem

Hand-copied mocks drift by construction. Both reviewers recommend generating the shipped-page mocks from the real components.

## Scope

- `/grill-with-docs` first: a `cmd/mockgen` that renders each page component with fixture view-models into `mockups/generated/`, viewer/writer/admin and empty/error variants per page; hand-written HTML stays only for unshipped designs in `.scratch/`.
- A CI job that fails when generated output changes without the mock being regenerated.

## Files

- `cmd/mockgen (new)`
- `mockups/`

## Acceptance criteria

- [ ] Generated mocks for every page in index.html
- [ ] CI check in place
- [ ] README explains when to hand-write vs generate

## Verification

Diff generated HTML against the live server's render for the same fixtures.

## Comments

2026-11-06 (triage): ready-for-human; design needs grilling before an agent can build it.

2026-11-12 (maintainer): wontfix. Mocks stay design-first and hand-written; the generator (#284) was reverted. See [`../../2026-11-12-mockups-design-first/spec.md`](../../2026-11-12-mockups-design-first/spec.md). Hand-mock drift is ticket 08 there.

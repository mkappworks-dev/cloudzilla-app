# Package registry

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/phase-16-package-registry` | `.worktrees/feat+phase-16-package-registry` | `feat(registry): package registry (16.1–16.2)` | 3 | L | Features +0.4 |

Mock: `../2026-11-06-package-registry/`

## Problem

Roadmap phase 16.1–16.2 is planned and has no UI.

## Scope

- `/grill-with-docs`, `/to-spec` and `/to-tickets` in `../2026-11-06-package-registry/`.
- Decisions: image path without repo segment (OCI source label), tags separate from manifests, `packages:read`/`packages:write` scopes, visibility follows the repo.

## Files

- per its spec

## Acceptance criteria

- [x] Spec and tickets exist (`../2026-11-06-package-registry/spec.md`, `issues/01`–`09`)
- [x] All its tickets done
- [x] ROADMAP status updated

## Verification

Per its spec.

## Comments

2026-11-06 (triage): ready-for-human; needs a spec first; mock decisions to confirm.

2026-11-19 (built): tickets 01-10 done on the integration branch `feat/phase-16-package-registry`; ROADMAP 16.1 and 16.2 are
marked done (migration 142) and `docs/package-registry.md` is the final page. The final PR is still to open.

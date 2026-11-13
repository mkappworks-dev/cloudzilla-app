# Signed commit verification

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/phase-17.2-signed-commits` | `.worktrees/feat+phase-17.2-signed-commits` | `feat: signed commit verification (17.2)` | 3 | L | Features +0.3 |

Mock: `../2026-11-06-security-signing-secrets/`

## Problem

Roadmap phase 17.2 is planned and has no UI.

## Scope

- `/grill-with-docs`, `/to-spec` and `/to-tickets` in `../2026-11-06-security-signing-secrets/`.
- Decisions from the mock README: "Partially verified" as a fourth status; SSH signing keys separate from auth keys.

## Files

- per its spec

## Acceptance criteria

- [x] Spec and tickets exist: [`../2026-11-06-security-signing-secrets/spec.md`](../../2026-11-06-security-signing-secrets/spec.md), tickets 01–08 in its `issues/`
- [ ] All its tickets done
- [ ] ROADMAP status updated

## Verification

Per its spec.

## Comments

2026-11-06 (triage): ready-for-human; needs a spec first; mock decisions to confirm.

Claude, 2026-11-13: specced. Build the eight tickets in `../2026-11-06-security-signing-secrets/issues/`; ticket 08 ticks the remaining criteria.

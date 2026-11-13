# Secret scanning

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/phase-17.3-secret-scanning` | `.worktrees/feat+phase-17.3-secret-scanning` | `feat: secret scanning (17.3)` | 3 | L | Features +0.3 |

Mock: `../2026-11-06-security-signing-secrets/`

## Problem

Roadmap phase 17.3 is planned and has no UI.

## Scope

- `/grill-with-docs`, `/to-spec` and `/to-tickets` in `../2026-11-06-security-signing-secrets/`.
- Decisions: open/closed + resolution reason vs ROADMAP's three-value state; push protection with "allow once"; auto-revoking leaked Cloudzilla tokens.

## Files

- per its spec

## Acceptance criteria

- [x] Spec and tickets exist: [spec-secret-scanning.md](../../2026-11-06-security-signing-secrets/spec-secret-scanning.md), tickets 09–17 in that folder
- [x] All its tickets done
- [x] ROADMAP status updated

## Verification

Per its spec.

## Comments

2026-11-06 (triage): ready-for-human; needs a spec first; mock decisions to confirm.

Claude, 2026-11-13 (spec): specced after a grill with the user. open/closed + resolution, one-line push-protection reason, allow = close (no timer), auto-revoke on any live Cloudzilla token match, fail-open scan budget. Build order and decisions are in the spec.

Claude, 2026-11-13 (built): tickets 09 to 17 are done on `feat/phase-17.3-secret-scanning`; ROADMAP 17.3 is marked done
(migrations 133 to 135); the subsystem doc is `docs/secret-scanning.md`.

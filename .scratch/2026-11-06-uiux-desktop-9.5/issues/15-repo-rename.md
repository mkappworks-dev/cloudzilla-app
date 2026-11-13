# Repository rename

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/repo-rename` | `.worktrees/feat+repo-rename` | `feat(repos): rename a repository` | 3 | L | Features +0.2 |

## Problem

Repo settings render the name read-only ("Renaming from settings is not yet available", `repo_settings.templ:168`).

## Scope

- `/to-spec` in a new `.scratch/<date>-repo-rename/` folder: redirects from the old path for web and git, webhooks and mirrors, forks.

## Files

- per its spec

## Acceptance criteria

- [x] Spec and tickets exist
- [x] All its tickets done
- [x] ROADMAP status updated

## Verification

Per its spec.

## Comments

2026-11-06 (triage): ready-for-human; needs a spec first.

2026-11-12 (spec session): spec written at [`.scratch/2026-11-12-repo-rename/spec.md`](../../2026-11-12-repo-rename/spec.md).
The build tickets (01–07) live there, not here; this ticket is done when they are, and ticket 07 marks it `done`.
Status stays `ready-for-human` until then. Org name change stays with ticket 34.

2026-11-13: all seven build tickets in the repo-rename spec are done; ROADMAP lists the feature under shipped outside the phase plan.

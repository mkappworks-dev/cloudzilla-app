# Commit identity, pulse and blame presentation

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/history-identity-pulse-blame` | `.worktrees/fix+history-identity-pulse-blame` | `fix(ui): commit identity, pulse and blame presentation` | polish (wave 2 re-score) | S | Content +0.2, Visual +0.1 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. The commit page shows the author as `<2+siteadmin@users.noreply.localhost>` and blame shows truncated relay addresses instead of the resolved username (Claude, `repo-commit`, `repo-blame`).
2. Pulse reads "Commits 0 total" for 30 days beside top contributors with 11, 10, 9 and 3 commits, an unlabeled all-time window, and grey placeholder bars that look like broken charts (Claude, `repo-pulse`).
3. Blame row heights are uneven around blank lines, continuation markers are nearly invisible, and the header counts 11 lines against 12 on the blob page.

## Scope

- Resolve the author to a username with a profile link, keep the address in a tooltip. Label the pulse windows consistently, drop placeholder bars or draw real ones. Fixed row height and a visible continuation marker in blame; one line count.
- Timestamps are ticket 31.

## Files

- `internal/view/pages/commit.templ`, `blame.templ`, `pulse.templ`
- matching mocks

## Acceptance criteria

- [x] Commit and blame show usernames
- [x] Pulse windows labeled and charts real or absent
- [x] Blame line counts agree with blob
- [x] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent.

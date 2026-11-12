# Account issue and PR lists use the qualifier bar

Created: 2026-11-12
Category: enhancement
Status: needs-triage
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/account-lists-qualifier-bar` | `.worktrees/feat+account-lists-qualifier-bar` | `feat(ui): account issue and PR lists use the qualifier bar` | polish (wave 2 re-score) | M | IA +0.2, Features +0.1 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. `/issues` and `/pulls` (the signed-in user's lists) ignore `q=` and show no search field or chips; they use their own `filter`, `state` and `sort` parameters (Codex and Claude, `account-issues-filtered`, `account-pulls-filtered`). The repository lists have the qualifier bar from ticket 10, so the same query works in one place and silently does nothing in the other.

## Scope

- Give the account lists the same `ListFilterBar`, with `is:`, `author:@me`, `assignee:@me`, `review:` and a repository qualifier, and map the existing filter tabs onto it.

## Files

- `internal/handler/account_*page*.go`, `internal/view/pages/account_issues.templ`, `account_pulls.templ`
- `internal/listquery/`
- `mockups/my_issues.html`, `my_pulls.html`

## Acceptance criteria

- [ ] A query typed on an account list is applied and shown as chips
- [ ] Old filter URLs keep working
- [ ] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

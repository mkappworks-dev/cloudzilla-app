# Fix display bugs on account lists, releases and the bell

Created: 2026-11-06
Category: bug
Status: ready-for-agent
Blocked by: 03

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/account-list-display-bugs` | `.worktrees/fix+account-list-display-bugs` | `fix(ui): private and draft markers on account lists, release excerpts, bell cap` | 1, lane C | S | Edge +0.6 |

## Problem

Verified in code: `/issues` never passes `Private` to `IssueListRow` (`account_issues.templ:59`); `/pulls` badge and icon ignore `IsDraft` (`account_pulls.templ:76,184`). The releases list prints raw Markdown (`releases.templ:73`). The bell shows "1492" uncapped. Client-side text filters on `/issues`, `/pulls`, `/repos`, `/attention`, gists leave an empty bordered box when nothing matches.

## Scope

- Pass `Private` on `/issues`; draft state on `/pulls` (reuse `pullStateBadge` logic from `pr_list_row`).
- Releases list: plain-text excerpt of the body, `line-clamp-2`.
- Bell badge shows "99+" above 99; `aria-label` keeps the exact count.
- A "No … match this filter." row that `x-show`s when every row is hidden.

## Files

- `internal/view/pages/account_issues.templ`
- `internal/view/pages/account_pulls.templ`
- `internal/view/pages/releases.templ`
- `internal/view/layout/layout.templ`
- `internal/view/pages/account_repos.templ`
- `internal/view/pages/account_attention.templ`
- `internal/view/pages/gists.templ`

## Acceptance criteria

- [ ] Private issues and draft PRs are marked on account lists
- [ ] Release cards show an excerpt, never Markdown syntax
- [ ] Bell caps at 99+
- [ ] Each client filter shows a no-match row

## Verification

Seeded server: siteadmin has private issues and draft PRs in the seed.

## Comments

2026-11-06 (triage): ready-for-agent.

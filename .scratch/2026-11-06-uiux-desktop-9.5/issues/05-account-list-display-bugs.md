# Fix display bugs on account lists, releases and the bell

Created: 2026-11-06
Category: bug
Status: done
Blocked by: 03, 04

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/account-list-display-bugs` | `.worktrees/fix+account-list-display-bugs` | `fix(ui): private and draft markers on account lists, release excerpts, bell cap` | 1, step 2 | S | Edge +0.6 |

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

- [x] Private issues and draft PRs are marked on account lists
- [x] Release cards show an excerpt, never Markdown syntax
- [x] Bell caps at 99+
- [x] Each client filter shows a no-match row

## Verification

Seeded server: siteadmin has private issues and draft PRs in the seed.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (implementation): done on `fix/account-list-display-bugs`. The seed gives siteadmin private issues but no
draft PRs in any `/pulls` tab; the visual check assigned two public drafts to siteadmin on the scratch DB. The bell
badge moved to the button's corner (`-top-1 -right-1`): at `top-1 right-1` a "99+" covered the whole icon.

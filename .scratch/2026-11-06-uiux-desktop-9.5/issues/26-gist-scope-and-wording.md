# Gist scope, privacy wording and revision copy

Created: 2026-11-06
Category: bug
Status: needs-info
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/gist-scope-and-wording` | `.worktrees/fix+gist-scope-and-wording` | `fix(ui): gist scope, privacy wording and revision copy` | polish (wave 1 re-score) | M | IA +0.2, Content +0.3 |

## Problem

Found by the wave 1 re-score (Codex).

1. The account subnav's "Gists" count (6, the user's own) leads to `/gists`, "Explore gists" with 40 public items,
   changing scope without saying so (`internal/view/fragments/account_subnav.templ:15`,
   `internal/view/pages/gists.templ:18`).
2. A "Private" gist is described as reachable by anyone with the link, which clashes with repository privacy wording
   (`gist_new.templ:97`, `gist_detail.templ:49`). Either call it "Unlisted" or enforce private access.
3. Editing copy promises revisions but no revision history is browsable (`gist_detail.templ:70`, `gist_edit.templ:106`).
   Already in [audit.md](../audit.md).

## Scope

- Point the account tab at the user's own gists (`/{owner}/gists`) and keep Explore as its own entry.
- Decide Unlisted vs enforced private (needs a maintainer call), then align copy on all three gist pages.
- Remove the revision promise or ship a history view.

## Files

- `internal/view/fragments/account_subnav.templ`, `internal/view/pages/gists.templ`, `gist_new.templ`,
  `gist_detail.templ`, `gist_edit.templ`
- `mockups/gists.html`, `gist_*.html`

## Acceptance criteria

- [ ] The profile's Gists tab lists that user's gists
- [ ] One term for link-only gists across list, new, edit and detail
- [ ] No copy promises revision history unless it is browsable

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): needs-info: maintainer to decide "Unlisted" vs enforced private gists before any copy change.

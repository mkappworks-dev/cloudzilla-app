# Heading, timestamp and release-excerpt consistency

Created: 2026-11-06
Category: enhancement
Status: needs-triage
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/ui-consistency-sweep` | `.worktrees/tech+ui-consistency-sweep` | `tech(ui): heading, timestamp and release-excerpt consistency` | polish (wave 1 re-score) | M | Visual +0.2, Content +0.2 |

## Problem

Smaller findings both reviewers raised:

- Equivalent task pages use different heading hierarchies: file edit a 20px medium heading, issue creation the larger
  page heading (`edit_file.templ:18`, `gist_detail.templ:30`).
- Release summaries flatten headings and bullet lists into one long sentence (`releases.templ:73`).
- Project boards show a filled destructive button on every row (`projects.templ:168`).
- Timestamps mix "3 minutes ago", "3m ago", "Nov 6, 2026" and a raw "03 Nov 26 17:55 +0100" on the repo header
  (Claude; find the formatter that emits the last one).
- (Claude, unverified) Whether unknown routes render a styled 404 and server errors a styled 500; only `forbidden.templ`
  was found.

## Scope

One pass per bullet. The timestamp rule should live in one helper that all pages call.

## Files

- `internal/view/pages/edit_file.templ`, `gist_detail.templ`, `releases.templ`, `projects.templ`
- the time-formatting helpers in `internal/view/`
- matching mocks

## Acceptance criteria

- [ ] One task-page heading style
- [ ] Release excerpts keep paragraph breaks or show a plain-text first paragraph
- [ ] Project delete moves into a secondary control
- [ ] One relative/absolute timestamp rule across list and detail pages
- [ ] 404 and 500 pages checked and styled

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

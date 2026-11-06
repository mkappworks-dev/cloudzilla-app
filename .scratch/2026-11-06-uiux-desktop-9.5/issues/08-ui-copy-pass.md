# Copy pass: casing, plurals, honest footer

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
Blocked by: 04, 05, 06, 07

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/ui-copy-pass` | `.worktrees/tech+ui-copy-pass` | `tech(ui): sentence case, pluralisation and footer copy` | 1, lane D | S | Content +1.35 |

## Problem

Title Case leftovers ("Pull Requests", "New Pull Request", "Explore Repositories", "Most Forked", "Audit Log", "Admin Settings", "Transfer Ownership"); unpluralised counts ("1 accounts", "1 total entries", "1 reviewers", "1 repositories"); lowercase "open" badge on detail vs "Open" in lists (`badge.templ:48-57`); hard-coded "all systems normal" (`layout.templ:289`); developer hint under every blob; "starred in"; issues search placeholder says "is:open" on Closed and advertises `label:`/`milestone:`/`author:` it can't parse (`issues.templ:60,160-162`); code search says "public" but searches all readable repos and shows a raw ts_rank (`code_search.templ:55,76`).

## Scope

- Add a `pluralize(n, one, many)` helper and use it everywhere a count renders.
- Sentence case for titles, headings, buttons, tabs.
- Footer shows version text only.
- Issues placeholder matches the tab; remove the qualifier claim until ticket 10 ships.
- Code search copy says "repositories you can read"; drop the score.

## Files

- `internal/view/**/*.templ (wide)`
- `mockups/*.html`

## Acceptance criteria

- [ ] `grep -rnE "[0-9]+ (accounts|repositories|reviewers|entries)"` style checks pass
- [ ] No Title Case page titles
- [ ] Footer has no status claim
- [ ] Mocks match

## Verification

Screenshot pass over every page in the harness list.

## Notes

Touches many files: start it only after lanes A–C merge, and keep it copy-only.

## Comments

2026-11-06 (triage): ready-for-agent.

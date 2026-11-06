# Copy pass: casing, plurals, honest footer

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 04, 05, 06, 07

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/ui-copy-pass` | `.worktrees/tech+ui-copy-pass` | `tech(ui): sentence case, pluralisation and footer copy` | 1, step 3 | S | Content +1.35 |

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

- [x] `grep -rnE "[0-9]+ (accounts|repositories|reviewers|entries)"` style checks pass
- [x] No Title Case page titles
- [x] Footer has no status claim
- [x] Mocks match

## Verification

Screenshot pass over every page in the harness list.

## Notes

Touches many files: start it only after 04, 05, 06 and 07 merge, and keep it copy-only.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (implementation): `components.Pluralize`/`Count` replace the per-file plural helpers. Also fixed beyond the
list: "Branches & Tags" and "IP Address", "starred in"/"forked in" on activity rows, and `Open`/`Closed` badges on
project cards (same `IssueStateBadge`). The issues filter placeholder reads "Search open issues"/"Search closed issues"
until ticket 10 adds qualifiers. Verified by crawling 49 real pages and 17 mocks at 1440 in light and dark on a seeded
server: no unpluralised "1 …s" count in text, titles, aria-labels or placeholders, and no Title Case headings, tabs or
buttons left beyond proper nouns ("Assertion Consumer Service", org display names).

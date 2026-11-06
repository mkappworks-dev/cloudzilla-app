# Fix mock drift and COVERAGE.md

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/mockups-drift-fixes` | `.worktrees/tech+mockups-drift-fixes` | `tech(mockups): match shipped pages and correct COVERAGE` | mocks | M | Mocks 7.0 → 8.3, fidelity → 8.8 |

## Problem

See audit.md "Systemic drift causes": two-letter initials, side-by-side base+caller classes that twMerge would drop, missing skip link/main/footer on releases and wiki mocks, wrong shell.js links, dark code blocks in light mode, and false COVERAGE.md claims (forms "submit and toast" but all use `onsubmit="return false"`; draft releases and the private-issue row ARE mocked; wrong routes in index.html).

## Scope

- Fix each systemic cause in `mockups/shell.js` and the affected pages.
- Apply twMerge results by hand (the conflicting base class removed).
- Use code-theme tokens for code blocks.
- Make forms and destructive actions actually toast/confirm, or correct COVERAGE.
- Add missing states COVERAGE omits (list in audit.md).

## Files

- `mockups/**`

## Acceptance criteria

- [ ] Each systemic cause fixed
- [ ] COVERAGE.md claims verified one by one
- [ ] index.html routes match router.go

## Verification

Screenshot every mock/real pair at 1440 light and dark and compare.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (wave 1 re-score, ticket 22): The polish PR edits these mocks, so expect a small merge: `pr.html` (merge
subtitle), `pr_files.html` (diff header `role=button` and keyboard toggle), `register.html` (password hint),
`admin_settings.html` (setting labels), `new_organization.html` (terms text, no link). Issue-list empty state and toast
dismiss are not drawn in the mocks.

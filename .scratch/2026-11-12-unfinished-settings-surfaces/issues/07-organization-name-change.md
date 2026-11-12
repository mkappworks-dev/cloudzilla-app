# Organization name change

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 05; uiux ticket 15 (repository rename) for the alias mechanism
Parallel: none; shares `org_settings.templ` with 04 and 05.

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/organization-rename` | `.worktrees/feat+organization-rename` | `feat(orgs): rename an organization` |

Open question, ask the ticket 15 session before building: does its alias table support owner-level aliases? If not,
extend it here.

## What

Rename per the [spec](../spec.md#organization-name-change), reusing 15's alias mechanism.

- Owner-only, with the dangerous-action confirmation. Same name rule and shared name space as creation.
- One transaction renames the `organizations` row and creates the alias; repository and wiki directories under
  `<repos_root>/<owner>/` move; ticket 15's rollback approach for a failed move applies.
- Old `/old/x` URLs, HTTP and SSH, redirect to `/new/x` until another User or Organization claims the old name.
- `org_settings.templ`: the Name field becomes editable with a rename action; remove "Organization names can't be
  changed yet."
- Check avatar storage keys, webhooks' payload `full_name`, and stored absolute URLs.

## Tests

Rename moves directories and keeps clones working via the alias; claiming the old name deletes the alias; name collision
with a User is refused; non-owner refused; a failed directory move rolls the row back.

## Mocks

`mockups/org_settings.html` profile section with the rename control. Light and dark at 1440.

## Acceptance criteria

- [ ] Rename works and old URLs redirect until claimed
- [ ] Docs (`docs/organizations.md`, `docs/access-control.md` name rules) and mock updated, light and dark

## Verification

Throwaway server on a scratch database at 1440×900, light and dark, per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness). Sign in as `admin@example.test` / `cloudzilla-seed`; `paper-planes` is the seeded organization. Rename `paper-planes`, then clone and browse the old URL.

## Comments

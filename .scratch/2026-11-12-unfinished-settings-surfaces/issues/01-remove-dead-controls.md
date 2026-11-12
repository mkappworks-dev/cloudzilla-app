# Remove dead controls

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —
Parallel: safe with 02 and 04. Shares `new_organization.templ`, `repo_fork.templ` and the `repoNewCustomSelect` component with no other ticket.

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/remove-dead-controls` | `.worktrees/feat+remove-dead-controls` | `feat(ui): remove dead organization and fork controls` |

## What

- `internal/view/pages/new_organization.templ`: delete the "This organization belongs to" and "Plan" fieldsets (the
  disabled "An enterprise account" and "Team / Billing not yet supported" radios). Keep one line, "You will be the owner
  of this organization." Check that the handler ignores the removed fields.
- `internal/view/pages/repo_fork.templ`, `internal/handler/fork_page_handler.go`: the Owner list keeps the current
  User first. When they own the source Repository their account stays in the list, disabled, with "Owns this
  repository"; the first enabled option is the default. `ErrForkIntoSourceOwner` stays. Keep the empty state for a User
  with no Organization to fork into. `repoNewCustomSelect` needs a disabled-option field; check other callers.
- Leave `comingSoonBadge` and `comingSoonButton` (`settings.templ`): tickets 02 and 03 own them.
- Do not touch the rename row in `repo_settings.templ` (uiux ticket 15).

## Migrations

None.

## Tests

- Handler test: the User is first and enabled when they do not own the source; disabled with the reason when they do;
  the default is the first enabled option.
- Page test: `/organizations/new` renders no `disabled` radio and no "Billing" text; posting without the removed
  fields still creates the Organization.

## Mocks

`mockups/new_organization.html` (drop both fieldsets), `mockups/repo_fork.html` (disabled Owner option). Light and dark
at 1440. Copy classes from the templ; check under `make mockups`.

## Acceptance criteria

- [x] No disabled radio or "Not available" text in organization creation
- [x] Fork Owner shows the User's account disabled with a reason when they own the source
- [x] Tests above pass; `make lint` and `npm run fmt:check` pass
- [x] Mocks updated, light and dark

## Verification

Throwaway server on a scratch database at 1440×900, light and dark, per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness). Sign in as `admin@example.test` / `cloudzilla-seed`; `paper-planes` is the seeded organization. Fork `siteadmin/rusty-meadow` as `siteadmin` (disabled option, org default) and an `ada-silva` repo as `siteadmin`
(User first).

## Comments

# Teams: create and members

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 04 (`org_settings.templ`)
Parallel: none with 06 or 07 (same file or a dependency). Safe with 01–03.

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/org-teams-members` | `.worktrees/feat+org-teams-members` | `feat(orgs): teams and team members` |

## What

Teams without any effect on access, per the [spec](../spec.md#teams). Read `docs/organizations.md` and
`docs/access-control.md`.

- Migration: `teams(id, org_id, name, slug, description, created_at)` with unique `(org_id, slug)`;
  `team_members(team_id, user_id)` primary key both.
- `TeamStore`/`TeamService` (wire into `Stores`, `Services`): create, rename, delete, add/remove member, list.
  Owner-only mutation. Members must be current Organization members. `OrgService.RemoveMember`, the member-leaves path
  and `DeleteUser` remove the User from every Team in the same transaction.
- Handlers: JSON and form POSTs in `team_handler.go`; a Teams page in `team_page_handler.go` (`PageOrgTeam`) at
  `/orgs/{org}/teams/{slug}`; list in `org_settings.templ` replaces the empty state. Any Organization member can see
  the list and members; mutation is owner-only. Routes in `router.go`. Add `teams` handling to the reserved-slug check
  only if a slug collides with a route.
- Audit log entries for create/rename/delete/membership changes if the org audit panel already records profile edits.
- Update `docs/organizations.md`.

## Tests

Service: non-owner refused; non-member cannot join; slug uniqueness and normalisation; removing a member or deleting a
User leaves no `team_members` row; deleting the Organization cascades. Handler: visibility to members vs outsiders.

## Mocks

`mockups/org_settings.html` Teams section (empty, list, add-member). A Team page mock is new: add
`mockups/org_team.html`, `index.html` and `COVERAGE.md`. Light and dark at 1440.

## Acceptance criteria

- [x] Owners create teams and edit members; members see them; outsiders cannot
- [x] Leaving or being removed from the Organization removes Team membership
- [x] No change to `CanRead`/`CanWrite`/`CanManage` yet
- [x] Docs and mocks updated, light and dark

## Verification

Throwaway server on a scratch database at 1440×900, light and dark, per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness). Sign in as `admin@example.test` / `cloudzilla-seed`; `paper-planes` is the seeded organization.

## Comments

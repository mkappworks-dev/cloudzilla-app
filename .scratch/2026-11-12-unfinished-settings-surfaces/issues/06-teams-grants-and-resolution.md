# Teams: repository grants and permission resolution

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 05
Parallel: none; touches the permission core, so run alone.

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/org-team-grants` | `.worktrees/feat+org-team-grants` | `feat(orgs): team repository grants and permission resolution` |

## What

Grants and the resolution rule in the [spec](../spec.md#teams). Read `docs/access-control.md` fully first.

- Migration: `team_repos(team_id, repo_id, role)` with role CHECK `reader|writer|admin`; cascades from team and repo.
- Resolution in `RepoService`: effective role = highest of the direct Collaborator role and all grants of the User's
  Teams. `CanRead`, `CanWrite` and `CanManage` use it; `IsOwner` is unchanged. Team `admin` cannot grant/revoke
  `admin`, transfer, archive or delete.
- SQL forms: extend `readableBy` (`internal/store/repo_store.go`) with an `EXISTS` over `team_repos` and
  `team_members`; `ownedBy` stays. List every caller (account issue/PR lists and counts, activity feed, attention
  inbox, repo/issue/PR/code search, notification digest) in the PR and test each. `issueVisibleTo` follows `CanWrite`.
- Git over HTTP and SSH and the API use the same checks; token scopes still narrow. Add a git push test with a
  team-only writer.
- Grant management: owner-only, on the Team page (add a Repository, choose role, remove) and listed on the repository's
  collaborator section as "via team `x`" read-only lines. Granting `admin` is owner-only.
- Transferring a Repository out of the Organization deletes its `team_repos`; deleting a Repository cascades.
- Update `docs/access-control.md` (roles table, "Authorization Checks", the member note "no implicit access") and
  `docs/organizations.md`.

## Tests

The matrix: direct only, team only, both (highest wins), two teams, removing the direct row keeps team access and the
reverse, public repo, private repo with no grant, team `admin` blocked from `admin` grants/transfer/delete. SQL form
parity: for each caller the result of `readableBy` equals `CanRead` over a seeded set. End-to-end: clone, push, PR
create and private issue visibility as a team-only member. Integration against `TEST_DATABASE_DSN`.

## Mocks

`mockups/org_team.html` (grants), repository settings collaborator list "via team" line: no standing repo-settings mock,
so record it in `mockups/COVERAGE.md`. Light and dark at 1440.

## Acceptance criteria

- [x] Highest-role-wins resolution everywhere `CanRead`/`CanWrite`/`CanManage` and their SQL forms are used
- [x] `IsOwner` and the `admin`-grant rule unchanged
- [x] Docs updated; all callers of `readableBy` listed and tested
- [x] Mocks updated, light and dark

## Verification

Throwaway server on a scratch database at 1440×900, light and dark, per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness). Sign in as `admin@example.test` / `cloudzilla-seed`; `paper-planes` is the seeded organization. Add `ada-silva` to a Team holding `writer` on a private repo and clone, push and open an issue as them.

## Comments

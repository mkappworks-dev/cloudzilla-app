# Organizations

Cloudzilla supports organization accounts. An org is a shared namespace that can own repositories and have multiple members with roles.

## Roles

| Role     | Description                                                                                                                               |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `owner`  | Full admin: edit the profile and repository defaults, add/remove members and change their roles, create repos, transfer or delete the org |
| `member` | Listed on the org profile; can leave the org; sees private org repos only where they hold a collaborator role; no org management rights   |

An org always keeps at least one owner: removing or demoting the last owner is rejected, and so is deleting the account of an org's only owner (see [Account deletion](#account-deletion)). Each of these changes locks the org's row before counting owners (`OrgStore.changeMember`; `UserStore.DeleteWithOwnedRepos` locks every org the user belongs to), so two owners leaving, demoting each other or deleting their accounts at once, or a member being promoted while deleting their account, cannot leave the org without an owner. Any member may remove themselves ("leave"); removing someone else requires the owner role.

## Repository ownership

An org repo belongs to the org, as on GitHub. Its `repositories.org_id` names the org and its `owner_id` is `NULL`; a `CHECK` constraint requires exactly one of the two. Access comes only from the org and from explicit repo roles:

- Org owners have owner-level access to every org repo.
- Members have no implicit access to private org repos; they need a collaborator role (`reader`/`writer`/`admin`) on each.
- `created_by` records who created a repo, org or personal, and grants nothing. It is `NULL` for repos whose creator's account was deleted, and migration 085 backfilled it from `owner_id`.

So a member who created a repo keeps only what the org and their explicit role give them: leaving the org or being demoted takes away the rest, over git, the web UI and the API alike. Deploy keys, webhooks and forks they set up belong to the repo, not to them, and stay until an owner removes them. The store queries that filter many repos by what the viewer can read (account issue and PR lists and counts, the activity feed, the attention inbox, repo search) use `readableBy` and `ownedBy` from `internal/store/repo_store.go`, the SQL forms of `RepoService.CanRead` and `IsOwner`.

A live org repo's name is unique within the org (`idx_repos_org_name_live`). A soft-deleted one does not hold its name, so `Restore` refuses when the name has been taken since. Any org owner may soft-delete, restore or transfer an org repo. Two owners can act on one repo at once, so a transfer or soft delete acts only on the row it read (same `owner_name`, still live) and fails with `ErrRepoChanged` otherwise.

## Account deletion

Deleting an account never deletes or detaches org repos, live or soft-deleted; the org keeps them and their directories, their `created_by` becomes `NULL`, and a `deleted_by` naming the user passes to the ghost user, as everything the user wrote in them does. The account's personal repos are deleted as described in [access-control](./access-control.md#account-deletion). Deletion is refused with `ErrSoleOrgOwner` (settings error `sole_org_owner`) while the user is the only owner of an organization; they add another owner or delete the org first. Being a plain member never blocks it.

## Pages

| Route                     | Auth       | Description                                                                                                                     |
| ------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `/organizations`          | Required   | The orgs you belong to, with your role and each org's member count                                                              |
| `/organizations/new`      | Required   | Create-org form (`name`, `description`, `accept_tos`); the terms checkbox is enforced server-side                               |
| `/{org}`                  | Optional   | Org profile: header, README, most-starred and recently updated repos, people, top languages                                     |
| `/{org}?tab=repositories` | Optional   | Every org repo the viewer can see                                                                                               |
| `/{org}?tab=people`       | Optional   | Every org member with their role; the profile sidebar shows the first 12                                                        |
| `/orgs/{org}/settings`    | Owner only | General (profile), Members, Repository defaults, Audit log, Danger zone (transfer, delete); Teams and Webhooks are placeholders |
| `/repos/new?owner={org}`  | Required   | New-repo form with the org preselected, if you own it, and its default visibility                                               |

The `/{owner}` route first checks if `owner` is a user; if not, falls back to org lookup. Org profile and user profile share the same URL pattern, so a name belongs to a user or an org, never both: org creation refuses a user's name, every account-creating path refuses an org's name, and database triggers enforce both directions (see [access-control](./access-control.md#usernames)).

The profile README is the `README.md` of the org's public repo named after the org (`{org}/{org}`), mirroring the user-profile convention. Org owners see every org repo; other viewers see public repos plus private ones where they hold a collaborator role. The repo cards and the top-languages bar follow the same visibility; languages count each visible repo's cached `primary_language` (`LanguageService.AggregateForOrg`, over the same filtered list as the cards).

## Profile

Owners edit these on the settings page (`POST /api/orgs/{org}/profile`):

| Field           | Notes                                                                                        |
| --------------- | -------------------------------------------------------------------------------------------- |
| `display_name`  | Shown as the profile heading; falls back to the org name                                     |
| `description`   | Shown under the heading                                                                      |
| `website`       | Must be `http` or `https`; a bare host such as `acme.dev` is stored as `https://acme.dev`    |
| `location`      | Free text                                                                                    |
| `contact_email` | Shown publicly on the org profile as a `mailto:` link. Not collected on `/organizations/new` |

The org name (URL slug) cannot be changed, and avatar upload is not implemented yet.

## Repository defaults

Owners set these on the settings page (`POST /api/orgs/{org}/repo-defaults`):

| Field                     | Values                   | Default   | Applied                                                                                                                                                       |
| ------------------------- | ------------------------ | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `default_repo_visibility` | `public`, `private`      | `private` | Preselected on `/repos/new` when the org is chosen as owner (an explicit `?visibility=` wins); used by `POST /api/orgs/{org}/repos` when `private` is omitted |
| `default_branch_name`     | Non-empty, no whitespace | `main`    | Initial branch of every repo created under the org                                                                                                            |

## Transfer and delete

- **Transfer** (`POST /api/orgs/{org}/transfer`): the named user becomes an owner (added as a member first if needed) and the requesting owner is demoted to member, then redirected to `/{org}`, since the settings page is now closed to them. The settings dialog requires typing the org name (`confirm_name`).
- **Delete** (`POST /api/orgs/{org}/delete`): requires `confirm_name` to equal the org name, and is refused with `ErrOrgHasRepos` while the org owns any live repository, because the `organizations` FK cascade would otherwise wipe every repo, issue, PR, and comment. `OrgStore.Delete` checks with the org row and its repo rows locked, so a repo created, transferred in or restored meanwhile blocks the delete instead of being cascaded away. Member rows cascade. Audit-log entries for the org are kept.

## Audit log

Profile edits, repository-default changes, and deletes write audit entries (`org.profile.update`, `org.defaults.update`, `org.delete`) with `target_type = 'org'` and `target_id` set to the org ID. The settings page lists the 25 most recent entries for the org.

## API Endpoints

| Method | Path                                      | Auth     | Description                                                                                                   |
| ------ | ----------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------- |
| POST   | `/api/orgs/`                              | Required | Create org (`name`, `display_name`, `description`)                                                            |
| GET    | `/api/orgs/{org}`                         | —        | Get org by name, including profile fields and repository defaults                                             |
| GET    | `/api/orgs/{org}/members`                 | —        | List org members                                                                                              |
| POST   | `/api/orgs/{org}/members`                 | Required | Add member (`username`, `role`); owner only                                                                   |
| DELETE | `/api/orgs/{org}/members/{username}`      | Required | Remove member; owner only, except a member may remove themselves; last owner blocked                          |
| POST   | `/api/orgs/{org}/members/{username}/role` | Required | Change a member's role (`role`: `owner` or `member`); owner only; last owner cannot be demoted                |
| POST   | `/api/orgs/{org}/repos`                   | Required | Create a repo under the org; owner only; omitted `private` falls back to `default_repo_visibility`            |
| POST   | `/api/orgs/{org}/transfer`                | Required | Transfer org ownership (`new_owner`, optional `confirm_name` form fields); owner only; demotes self to member |
| POST   | `/api/orgs/{org}/profile`                 | Required | Update profile fields (form post); owner only                                                                 |
| POST   | `/api/orgs/{org}/repo-defaults`           | Required | Update repository defaults (form post); owner only                                                            |
| POST   | `/api/orgs/{org}/delete`                  | Required | Delete the org (`confirm_name` form field); owner only; refused while the org owns repositories               |

HTMX requests to add, remove, or change the role of a member respond with the `OrgMembers` fragment, which the settings page swaps into `#org-members-list`. See [api-reference.md](api-reference.md#organizations) for redirects and field validation.

## OrgService (`internal/service/org_service.go`)

- `Create(ctx, creatorUserID, name, displayName, description)` → `(*Organization, error)` — returns `ErrInvalidOrgName` when `name` fails `ValidateName`, `ErrOrgNameTaken` when it matches a user or another org; auto-adds creator as owner
- `Get(ctx, name)` → `(*Organization, error)`
- `UpdateProfile(ctx, orgID, requestingUserID, displayName, description, website, location, contactEmail)` → `error` — owner-only; normalizes and validates `website`
- `UpdateRepoDefaults(ctx, orgID, requestingUserID, visibility, branchName)` → `error` — owner-only; validates both values
- `Delete(ctx, orgID, requestingUserID)` → `error` — owner-only; `ErrOrgHasRepos` while the org owns repos
- `ListMembers(ctx, orgID)` → `([]OrgMember, error)`
- `CountMembers(ctx, orgID)` → `(int, error)`
- `ListMembershipsForUser(ctx, userID)` → `([]OrgMembership, error)` — every org the user belongs to, tagged with their role; drives `/organizations`, the workspace switcher, and the org list on user profiles
- `ListOwnedByUser(ctx, userID)` → `([]Organization, error)` — orgs where the user is an owner; drives the `/repos/new` owner picker
- `IsOwner(ctx, orgID, userID)` → `bool`
- `IsMember(ctx, orgID, userID)` → `bool`
- `AddMember(ctx, orgID, requestingUserID, targetUserID, role)` → `error` — owner-only
- `UpdateMemberRole(ctx, orgID, requestingUserID, targetUserID, role)` → `error` — owner-only; blocks demoting the last owner, checked under the org row lock
- `RemoveMember(ctx, orgID, requestingUserID, targetUserID)` → `error` — owner-only unless removing self; blocks removing last owner, checked under the org row lock
- `CreateRepo(ctx, orgID, requestingUserID, name, description, private, init)` → `(*Repository, error)` — owner-only; sets `owner_name` to org name, `org_id` to org ID, `created_by` to the requester (`owner_id` stays `NULL`), and the default branch to the org's `default_branch_name`; `init` seeds README/.gitignore/LICENSE; returns `ErrRepoNameTaken` when the org already has a live repo with that name or a directory for it is left on disk, and `ErrRepoNameReserved` for a name ending in `.wiki`
- `ListRepos(ctx, orgID)` → `([]Repository, error)`
- `ListReposVisibleTo(ctx, orgID, viewerID)` → `([]Repository, error)` — all repos for org owners; public plus collaborator-accessible private repos for everyone else
- `RepoHighlights(ctx, repos, featuredLimit, recentLimit)` → `(*OrgRepoHighlights, error)` — ranks repos the caller already filtered for the viewer: `Featured` is the most-starred public repos (orgs have no pin storage yet), `Recent` is newest-updated first and skips featured ones
- `TransferOrg(ctx, orgID, requestingUserID, newOwnerUsername)` → `error` — owner-only; promotes new user to `owner`, demotes requesting user to `member`; adds new user as member if not already one; the demotion is refused if it would leave no owner, for example when the new owner's account was deleted in between

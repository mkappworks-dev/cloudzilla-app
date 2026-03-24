# Organizations

Cloudzilla supports organization accounts. An org is a shared namespace that can own repositories and have multiple members with roles.

## Roles

| Role     | Description                                                               |
| -------- | ------------------------------------------------------------------------- |
| `owner`  | Full admin: add/remove members, create/manage all repos in the org        |
| `member` | Can view org profile and be listed as a member; no repo management rights |

## Pages

| Route                  | Auth       | Description                      |
| ---------------------- | ---------- | -------------------------------- |
| `/{org}`               | Optional   | Org profile: repos + member list |
| `/orgs/{org}/settings` | Owner only | Manage members (add/remove)      |

The `/{owner}` route first checks if `owner` is a user; if not, falls back to org lookup. Org profile and user profile share the same URL pattern.

## API Endpoints

| Method | Path                                 | Auth     | Description                                                                         |
| ------ | ------------------------------------ | -------- | ----------------------------------------------------------------------------------- |
| POST   | `/api/orgs/`                         | Required | Create org (`name`, `display_name`, `description`)                                  |
| GET    | `/api/orgs/{org}`                    | —        | Get org by name                                                                     |
| GET    | `/api/orgs/{org}/members`            | —        | List org members                                                                    |
| POST   | `/api/orgs/{org}/members`            | Required | Add member (`username`, `role`); owner only                                         |
| DELETE | `/api/orgs/{org}/members/{username}` | Required | Remove member; owner only; last owner blocked                                       |
| POST   | `/api/orgs/{org}/repos`              | Required | Create a repo under the org; owner only                                             |
| POST   | `/api/orgs/{org}/transfer`           | Required | Transfer org ownership (`new_owner` form field); owner only; demotes self to member |

HTMX responses from add/remove member swap `fragment-org-members` into `#org-members`.

## OrgService (`internal/service/org_service.go`)

- `Create(ctx, creatorUserID, name, displayName, description)` → `(*Organization, error)` — validates name uniqueness against users table; auto-adds creator as owner
- `Get(ctx, name)` → `(*Organization, error)`
- `ListMembers(ctx, orgID)` → `([]OrgMember, error)`
- `IsOwner(ctx, orgID, userID)` → `bool`
- `IsMember(ctx, orgID, userID)` → `bool`
- `AddMember(ctx, orgID, requestingUserID, targetUserID, role)` → `error` — owner-only
- `RemoveMember(ctx, orgID, requestingUserID, targetUserID)` → `error` — owner-only; blocks removing last owner
- `CreateRepo(ctx, orgID, requestingUserID, name, description, private)` → `(*Repository, error)` — owner-only; sets `owner_name` to org name, `org_id` to org ID
- `ListRepos(ctx, orgID)` → `([]Repository, error)`
- `TransferOrg(ctx, orgID, requestingUserID, newOwnerUsername)` → `error` — owner-only; promotes new user to `owner`, demotes requesting user to `member`; adds new user as member if not already one

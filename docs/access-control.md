# Access Control — Instance, Organization & Repository Permissions

## Permission Levels

| Level        | Roles                       | Description                               |
| ------------ | --------------------------- | ----------------------------------------- |
| Instance     | `superadmin`, `user`        | Controls instance-wide access             |
| Organization | `owner`, `member`           | Controls org membership and repo creation |
| Repository   | `reader`, `writer`, `admin` | Controls per-repo access                  |

## Instance Roles

| Role         | Permissions                                                                        |
| ------------ | ---------------------------------------------------------------------------------- |
| `superadmin` | Everything. Manages instance settings, cannot be locked out. Assigned at `/setup`. |
| `user`       | Normal account. Access governed by org/repo permissions and instance settings.     |

## First-Run Wizard (`/setup`)

When no users exist, **all routes redirect to `/setup`**. The first person to submit the form becomes superadmin. After any user exists, `/setup` permanently redirects to `/`.

`RequireSetup` middleware runs globally (after Recoverer, before routes). Always passes `/setup`, `/static/`, `/invite/`, and `/htmx.min.js` through unconditionally.

`SiteSettingService.IsSetupComplete()` caches the result atomically via `sync/atomic.Bool` — once true it never re-queries the DB.

## Instance Settings (`site_settings` table)

Seeded by migration 014. Two boolean keys:

| Key                  | Default | Behaviour when `false`                                                        |
| -------------------- | ------- | ----------------------------------------------------------------------------- |
| `allow_registration` | `true`  | Blocks new account creation (form & OAuth). Invite tokens bypass this.        |
| `allow_login`        | `true`  | Blocks non-superadmin, non-invited logins. "Sign in" link hidden from navbar. |

## Invitation System

No SMTP required. Superadmin generates a token link → shares it manually.

Flow:

1. Superadmin POSTs `email` to `/api/admin/invitations` → 32-byte hex token, 7-day expiry
2. Admin panel displays `/invite/{token}` link for copying
3. Recipient visits link → form with `email` pre-filled (read-only)
4. On submit: user created via `UserService.Create`, `is_invited = TRUE` set, invitation marked accepted, JWT cookie set → redirect `/`

`is_invited` users always bypass `allow_registration` and `allow_login` checks.

## Admin Panel (`/admin/settings`)

Superadmin-only. Accessible via the "Admin" link (yellow) in the navbar.

- **Settings section**: HTMX toggle buttons; POST to `/api/admin/settings`; swaps `fragment-admin-settings` into `#admin-settings-list`
- **Invitations section**: create by email; displays invite URL for copying; delete pending invites; swaps `fragment-admin-invitations` into `#admin-invitations-list`

## Repository Collaborators

The `permissions` table has always existed; it now has full CRUD via the repo settings page.

`CanManage(ctx, repo, userID)` — gates collaborator management: **only** `repo.OwnerID == userID` or an org owner (`isOrgOwner`). Collaborators with the `admin` role have write access but cannot manage collaborators.

`ListPermissionsWithUsername` — uses `JOIN users ON p.user_id = u.id`; populates `Permission.Username` (not a DB column, populated via JOIN).

**Permission matrix for personal repos:**

| Who                          | Read private | Push | Manage collaborators | Transfer ownership |
| ---------------------------- | :----------: | :--: | :------------------: | :----------------: |
| Repo owner (`repo.owner_id`) |      ✓       |  ✓   |          ✓           |         ✓          |
| Collaborator: `admin`        |      ✓       |  ✓   |          ✗           |         ✗          |
| Collaborator: `writer`       |      ✓       |  ✓   |          ✗           |         ✗          |
| Collaborator: `reader`       |      ✓       |  ✗   |          ✗           |         ✗          |

For org repos, any org `owner` additionally gets read/write/manage on all repos in that org (via `isOrgOwner`). Transfer is not supported on org repos.

**API endpoints** (all under `/api/repos/{owner}/{repo}/collaborators`):

| Method                            | Auth                       | Description                           |
| --------------------------------- | -------------------------- | ------------------------------------- |
| GET `/collaborators`              | Optional                   | List collaborators with username      |
| POST `/collaborators`             | Required + owner/org-owner | Add collaborator (`username`, `role`) |
| DELETE `/collaborators?user_id=N` | Required + owner/org-owner | Remove collaborator                   |

**Ownership transfer** (personal repos only):

| Method | Path                                 | Auth             | Description                                                              |
| ------ | ------------------------------------ | ---------------- | ------------------------------------------------------------------------ |
| POST   | `/api/repos/{owner}/{repo}/transfer` | Required + owner | Transfer to another user (`new_owner` form field); moves git dir on disk |

HTMX responses swap `fragment-repo-collaborators` into `#repo-collaborators`.

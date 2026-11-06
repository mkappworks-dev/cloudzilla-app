# 1. Superadmins reach repositories and organizations through a time-boxed admin mode

Status: accepted, 2026-11-06

## Context

The seeded superadmin got `403` on other users' repository and organization settings, and had no way to repair a
repository they don't own. No `CanRead`, `CanWrite`, `CanManage` or organization check has a superadmin case, so the
instance role covered accounts, SSO and settings but nothing inside a repository or an organization.

## Decision

A superadmin is a normal user on every repository and organization, until they enter **admin mode**. There is no blanket
override. The model is GitLab's Admin Mode.

- **Per browser, server-side.** Entering sets a separate random cookie (`<auth.cookie_name>_admin`). Migration 117's
  `admin_modes` table holds the user, the token's SHA-256 (never the token), `created_at`, `expires_at` and `ended_at`,
  plus the `session_version` the user had at entry. Only that browser gets admin rights.
- **Entering takes the identity confirmation** (password plus 2FA or the provider factor, throttled by `ReauthService`),
  as `admin.org.owner_add` does.
- **One hour**, from `auth.admin_mode_ttl`.
- **It ends at once** on "Leave admin mode", logout, and any `session_version` bump (sign out other sessions, a password
  change or reset, suspension), because the row is only live while its `session_version` is the user's. It also ends
  when the user stops being a superadmin or is suspended. `Resolve` ends and audits a row it finds dead.
- **Cookie sessions only.** PATs, OAuth-app tokens, a session JWT sent as a bearer header, git smart HTTP and SSH never
  get it. Git routes use an auth middleware built without the option.
- **Bound to the user.** The middleware puts the admin-mode user's id in the request context (`internal/adminmode`).
  `RepoService.IsOwner`, `CanRead`, `CanWrite`, `CanManage` and `OrgService.IsOwner` and `IsMember` short-circuit only
  when the `userID` being checked equals that id, and only after the ordinary check has failed. A check run for anyone
  else inside the same request (notification recipients, webhook targets) is unchanged, so a goroutine that keeps the
  request context stays correct.
- **Audited.** `admin.mode.enter`; `admin.mode.leave` with a reason (`left`, `expired`, `revoked`); and
  `admin.mode.action` for each POST, PUT, PATCH or DELETE that passed only because of the override, with method, path,
  repository or organization, and status. The override marks the context when it is used, so a request that the normal
  checks allowed isn't logged. Viewing a private repository is not logged.
- **Visible.** A banner on every page while active, an "Enter admin mode" entry in the user menu, and a hint on the 403
  of repository and organization settings.

## Consequences

- Admin mode includes owner-only actions (transfer, delete, the `admin` role), because `IsOwner` is covered. The
  confirmation and the audit entry are the guard.
- Store queries that filter many repositories by what the viewer can read (`readableBy`, `ownedBy`: lists, search, the
  feed) are SQL and don't see admin mode. Opening a repository directly works; listing it among the admin's own does
  not.
- A new permission check that bypasses `RepoService` / `OrgService` (a raw `OwnerID == userID` comparison) won't honour
  admin mode, which fails closed.

## Alternatives rejected

- **A blanket superadmin override** on every check: no time limit, no audit, and every stolen superadmin session reads
  and rewrites every private repository.
- **A context flag meaning "everything passes"**: checks run for other users in the same request, so it would widen
  them.
- **An instance-wide admin mode**: it would give every browser of the account the rights.

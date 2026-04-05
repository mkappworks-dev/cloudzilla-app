# Access Control — Authentication, Authorization, Roles & Permissions

## Authentication Methods

| Method                | Mechanism                                          | Where                         |
| --------------------- | -------------------------------------------------- | ----------------------------- |
| Form login            | Email + password → JWT cookie (`cz_token`)         | `POST /login`                 |
| API login             | Email + password → JWT in cookie + JSON body       | `POST /api/auth/login`        |
| Google OAuth          | OAuth 2.0 code flow → JWT cookie                   | `GET /auth/google` → callback |
| LDAP                  | Bind + search → JWT cookie                         | `POST /auth/ldap`             |
| SAML SSO              | SP-initiated, ACS callback → JWT cookie            | `GET /auth/saml` → callback   |
| Personal Access Token | `Authorization: Bearer <token>` header             | Any API endpoint              |
| OAuth App Token       | `Authorization: Bearer <token>` header             | Scoped API endpoints          |
| SSH Public Key        | Key fingerprint lookup in `ssh_keys`/`deploy_keys` | Git SSH transport             |
| TOTP 2FA              | 6-digit code after password login                  | `POST /auth/2fa/verify`       |

### JWT Claims

All authenticated requests carry JWT claims in context:

```go
type Claims struct {
    UserID       int64
    Username     string
    IsSuperadmin bool
}
```

Extracted via `middleware.ClaimsFromContext(r.Context())`.

### Middleware Chain

```
Request → RequestID → Recoverer → Logger → CORS → CSRF → RequireSetup
                                                            ↓
                                              Route-specific middleware:
                                              - authMW (required auth)
                                              - optAuthMW (optional auth)
                                              - superadminMW (superadmin only)
                                              - apiBodyLimit (1 MB limit)
```

- **authMW**: Reads JWT from `Authorization: Bearer` header OR `cz_token` httpOnly cookie. Also accepts PATs and OAuth tokens. Returns 401 if missing/invalid.
- **optAuthMW**: Same as authMW but allows unauthenticated requests through. Claims may be nil.
- **superadminMW**: Requires `claims.IsSuperadmin == true`. Returns 403 otherwise.
- **CSRF**: Double-submit cookie pattern. Skips git transport, Bearer-auth, and safe methods (GET/HEAD/OPTIONS).

---

## Permission Levels

| Level        | Roles                       | Description                               |
| ------------ | --------------------------- | ----------------------------------------- |
| Instance     | `superadmin`, `user`        | Controls instance-wide access             |
| Organization | `owner`, `member`           | Controls org membership and repo creation |
| Repository   | `reader`, `writer`, `admin` | Controls per-repo access                  |

### Instance Roles

| Role         | Permissions                                                                                                     |
| ------------ | --------------------------------------------------------------------------------------------------------------- |
| `superadmin` | Everything. Manages instance settings, SSO, invitations, audit log. Cannot be locked out. Assigned at `/setup`. |
| `user`       | Normal account. Access governed by org/repo permissions and instance settings.                                  |

### Organization Roles

| Role     | Read public repos | Read private repos | Create repos | Manage members | Transfer org | Delete org |
| -------- | :---------------: | :----------------: | :----------: | :------------: | :----------: | :--------: |
| `owner`  |        Yes        |        Yes         |     Yes      |      Yes       |     Yes      |    Yes     |
| `member` |        Yes        |   No (need role)   |      No      |       No       |      No      |     No     |

Org members do not get implicit access to private repos. They must be added as explicit collaborators (`reader`/`writer`/`admin`) on each repo.

### Repository Roles

| Role                     | Read (public) | Read (private) | Push / Write | Manage (collabs, settings) | Transfer | Delete |
| ------------------------ | :-----------: | :------------: | :----------: | :------------------------: | :------: | :----: |
| Anyone (unauthenticated) |      Yes      |       No       |      No      |             No             |    No    |   No   |
| Authenticated (no role)  |      Yes      |       No       |      No      |             No             |    No    |   No   |
| `reader`                 |      Yes      |      Yes       |      No      |             No             |    No    |   No   |
| `writer`                 |      Yes      |      Yes       |     Yes      |             No             |    No    |   No   |
| `admin`                  |      Yes      |      Yes       |     Yes      |            Yes             |    No    |   No   |
| Repo owner               |      Yes      |      Yes       |     Yes      |            Yes             |   Yes    |  Yes   |
| Org owner (org repos)    |      Yes      |      Yes       |     Yes      |            Yes             |   Yes    |  Yes   |

**Manage** includes: collaborator CRUD, branch protection, deploy keys, topics, wiki deletion, discussion categories, webhook CRUD, repo settings page access.

**Transfer/Delete** (owner-only) includes: repo transfer, archive, unarchive, template toggle, soft-delete/restore.

### Organization Repo Ownership

For org repos, `owner_id` points to the org entity. Access is determined by `org_members`:

| Org Role | Create repos | Manage repos | Transfer repos | Delete repos | Appoint admins |
| -------- | :----------: | :----------: | :------------: | :----------: | :------------: |
| `owner`  |     Yes      |     Yes      |      Yes       |     Yes      |      Yes       |
| `member` |      No      |      No      |       No       |      No      |       No       |

Org owners can also appoint `admin` collaborators who can manage settings and assign `reader`/`writer` roles.

---

## Authorization Checks in Code

Four methods on `RepoService` enforce repository permissions:

```go
// CanRead — public repos always pass; private require auth + any role
func (s *RepoService) CanRead(ctx, repo, userID *int64) bool

// CanWrite — owner, org owner, or writer/admin collaborator
func (s *RepoService) CanWrite(ctx, repo, userID int64) bool

// CanManage — owner, org owner, or admin collaborator
func (s *RepoService) CanManage(ctx, repo, userID int64) bool

// IsOwner — repo owner or org owner only (for transfer, delete, archive)
func (s *RepoService) IsOwner(ctx, repo, userID int64) bool
```

### Two-Layer Enforcement Pattern

Every state-mutating endpoint enforces authorization at two levels:

1. **Router middleware** (`authMW`) — proves identity (authentication)
2. **Handler or service** (`CanWrite`/`CanManage`) — proves permission (authorization)

```go
// Example: handler-level authorization
func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
    claims, ok := middleware.ClaimsFromContext(r.Context())  // Layer 1: authn
    if !ok { writeError(w, 401, "unauthorized"); return }

    repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
    if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {  // Layer 2: authz
        writeError(w, 403, "forbidden"); return
    }
    // ... proceed with mutation
}
```

Some handlers delegate authorization to the service layer (e.g., ProjectService checks `CanWrite` internally with the passed `userID`).

---

## First-Run Wizard (`/setup`)

When no users exist, **all routes redirect to `/setup`**. The first person to submit the form becomes superadmin. After any user exists, `/setup` permanently redirects to `/`.

`RequireSetup` middleware runs globally. Always passes `/setup`, `/static/`, `/invite/`, and `/htmx.min.js` through unconditionally.

`SiteSettingService.IsSetupComplete()` caches the result atomically via `sync/atomic.Bool` — once true it never re-queries the DB.

## Instance Settings (`site_settings` table)

| Key                  | Default | When `false`                                                                  |
| -------------------- | ------- | ----------------------------------------------------------------------------- |
| `allow_registration` | `true`  | Blocks new account creation (form & OAuth). Invite tokens bypass this.        |
| `allow_login`        | `true`  | Blocks non-superadmin, non-invited logins. "Sign in" link hidden from navbar. |

## Invitation System

Superadmin generates token link → shares manually. No SMTP required.

1. Superadmin POSTs `email` to `/api/admin/invitations` → 32-byte hex token, 7-day expiry
2. Admin panel displays `/invite/{token}` link for copying
3. Recipient visits link → form with `email` pre-filled (read-only)
4. On submit: user created, `is_invited = TRUE` set, JWT cookie set → redirect `/`

`is_invited` users always bypass `allow_registration` and `allow_login` checks.

---

## Full Endpoint Authorization Matrix

### Instance / Admin Endpoints

| Method   | Path                          | Auth                  | AuthZ           | Handler                         |
| -------- | ----------------------------- | --------------------- | --------------- | ------------------------------- |
| GET      | `/admin/settings`             | authMW + superadminMW | Superadmin only | PageAdminSettings               |
| GET      | `/admin/audit-log`            | authMW + superadminMW | Superadmin only | PageAuditLog                    |
| GET/POST | `/admin/sso`                  | authMW + superadminMW | Superadmin only | PageSSOSettings / SaveSSOConfig |
| POST     | `/api/admin/settings`         | authMW + superadminMW | Superadmin only | UpdateSiteSetting               |
| POST     | `/api/admin/invitations`      | authMW + superadminMW | Superadmin only | CreateInvitation                |
| DELETE   | `/api/admin/invitations/{id}` | authMW + superadminMW | Superadmin only | DeleteInvitation                |

### Authentication Endpoints (No Auth Required)

| Method   | Path                    | Handler                       |
| -------- | ----------------------- | ----------------------------- |
| GET/POST | `/setup`                | PageSetup / PageSetupSubmit   |
| GET/POST | `/invite/{token}`       | PageInvite / PageInviteSubmit |
| GET/POST | `/login`                | PageLogin / PageLoginSubmit   |
| POST     | `/api/auth/login`       | Login                         |
| POST     | `/api/auth/logout`      | Logout                        |
| GET      | `/auth/google`          | GoogleOAuthBegin              |
| GET      | `/auth/google/callback` | GoogleOAuthCallback           |
| POST     | `/auth/ldap`            | LDAPLogin                     |
| GET/POST | `/auth/saml`            | InitiateSAML / SAMLCallback   |
| GET/POST | `/auth/2fa`             | PageTOTPVerify / VerifyTOTP   |

### User-Scoped Endpoints (Own Data Only)

| Method                | Path                             | Auth   | AuthZ                     | Handler                  |
| --------------------- | -------------------------------- | ------ | ------------------------- | ------------------------ |
| GET                   | `/settings`                      | authMW | Own user                  | PageSettings             |
| GET/POST              | `/settings/notifications`        | authMW | Own user                  | PageNotificationSettings |
| GET                   | `/settings/security`             | authMW | Own user                  | PageSecuritySettings     |
| POST                  | `/api/user/totp/enable`          | authMW | Own user (claims.UserID)  | EnableTOTP               |
| POST                  | `/api/user/totp/disable`         | authMW | Own user (claims.UserID)  | DisableTOTP              |
| GET/POST/DELETE       | `/api/user/keys`                 | authMW | Own user (claims.UserID)  | SSH key CRUD             |
| GET/POST/DELETE       | `/api/user/tokens`               | authMW | Own user (claims.UserID)  | Token CRUD               |
| GET/POST/PATCH/DELETE | `/api/user/replies`              | authMW | Own user (claims.UserID)  | Saved reply CRUD         |
| POST/DELETE           | `/api/oauth/apps`                | authMW | Own user (claims.UserID)  | OAuth app CRUD           |
| DELETE                | `/api/oauth/authorizations/{id}` | authMW | Own user (claims.UserID)  | RevokeOAuthAuthorization |
| POST/PATCH/DELETE     | `/api/gists`                     | authMW | Own gist (service checks) | Gist CRUD                |

### Organization Endpoints

| Method | Path                                 | Auth      | AuthZ                  | Handler         |
| ------ | ------------------------------------ | --------- | ---------------------- | --------------- |
| GET    | `/api/orgs/{org}`                    | optAuthMW | Public                 | GetOrg          |
| GET    | `/api/orgs/{org}/members`            | optAuthMW | Public                 | ListOrgMembers  |
| POST   | `/api/orgs`                          | authMW    | Any authenticated user | CreateOrg       |
| POST   | `/api/orgs/{org}/members`            | authMW    | Org owner (service)    | AddOrgMember    |
| DELETE | `/api/orgs/{org}/members/{username}` | authMW    | Org owner (service)    | RemoveOrgMember |
| POST   | `/api/orgs/{org}/repos`              | authMW    | Org owner (service)    | CreateOrgRepo   |
| POST   | `/api/orgs/{org}/transfer`           | authMW    | Org owner (service)    | TransferOrg     |

### Repository Endpoints — Read

| Method | Path                                             | Auth      | AuthZ                | Handler           |
| ------ | ------------------------------------------------ | --------- | -------------------- | ----------------- |
| GET    | `/api/repos`                                     | optAuthMW | Public list          | ListRepos         |
| GET    | `/api/repos/{owner}/{repo}`                      | optAuthMW | CanRead              | GetRepo           |
| GET    | `/{owner}/{repo}/issues`                         | optAuthMW | CanRead              | ListIssues        |
| GET    | `/{owner}/{repo}/issues/{number}`                | optAuthMW | CanRead + visibility | GetIssue          |
| GET    | `/{owner}/{repo}/pulls`                          | optAuthMW | CanRead              | ListPulls         |
| GET    | `/{owner}/{repo}/pulls/{number}`                 | optAuthMW | CanRead              | GetPull           |
| GET    | `/{owner}/{repo}/tree/blob/blame/commits/commit` | optAuthMW | CanRead              | Code browser      |
| GET    | `/{owner}/{repo}/releases`                       | optAuthMW | CanRead              | ListReleases      |
| GET    | `/{owner}/{repo}/milestones`                     | optAuthMW | CanRead              | ListMilestones    |
| GET    | `/{owner}/{repo}/labels`                         | optAuthMW | CanRead              | ListLabels        |
| GET    | `/{owner}/{repo}/hooks`                          | optAuthMW | CanWrite (handler)   | ListWebhooks      |
| GET    | `/{owner}/{repo}/collaborators`                  | optAuthMW | Public list          | ListCollaborators |

### Repository Endpoints — Write (Require CanWrite)

| Method            | Path                                                      | Auth   | AuthZ Check             | Handler                     |
| ----------------- | --------------------------------------------------------- | ------ | ----------------------- | --------------------------- |
| POST              | `/api/repos/{owner}/{repo}/issues`                        | authMW | CanWrite (handler)      | CreateIssue                 |
| PATCH             | `/api/repos/{owner}/{repo}/issues/{number}`               | authMW | CanWrite (handler)      | UpdateIssue                 |
| POST              | `/api/repos/{owner}/{repo}/issues/{number}/comments`      | authMW | CanWrite (handler)      | CreateIssueComment          |
| PATCH             | `/api/repos/{owner}/{repo}/issues/{number}/comments/{id}` | authMW | Author OR CanWrite      | UpdateComment               |
| DELETE            | `/api/repos/{owner}/{repo}/issues/{number}/comments/{id}` | authMW | Author OR CanManage     | DeleteComment               |
| POST              | `/api/repos/{owner}/{repo}/pulls`                         | authMW | CanWrite (handler)      | CreatePull                  |
| PATCH             | `/api/repos/{owner}/{repo}/pulls/{number}`                | authMW | CanWrite (handler)      | UpdatePull                  |
| POST              | `/api/repos/{owner}/{repo}/pulls/{number}/reviews`        | authMW | CanWrite (handler)      | SubmitReview                |
| POST/PATCH/DELETE | `.../pulls/{number}/line_comments`                        | authMW | CanWrite (handler)      | Line comment CRUD           |
| POST              | `/api/repos/{owner}/{repo}/labels`                        | authMW | CanWrite (handler)      | CreateLabel                 |
| DELETE            | `/api/repos/{owner}/{repo}/labels/{id}`                   | authMW | CanWrite (handler)      | DeleteLabel                 |
| POST/DELETE       | `.../issues/{number}/labels/{labelID}`                    | authMW | CanWrite (handler)      | Add/RemoveIssueLabel        |
| POST/DELETE       | `.../pulls/{number}/labels/{labelID}`                     | authMW | CanWrite (handler)      | Add/RemovePullLabel         |
| POST/DELETE       | `.../issues/{number}/assignees`                           | authMW | CanWrite (handler)      | Add/RemoveIssueAssignee     |
| POST/DELETE       | `.../pulls/{number}/assignees`                            | authMW | CanWrite (handler)      | Add/RemovePullAssignee      |
| POST              | `/api/repos/{owner}/{repo}/milestones`                    | authMW | CanWrite (handler)      | CreateMilestone             |
| PATCH             | `/api/repos/{owner}/{repo}/milestones/{number}`           | authMW | CanWrite (handler)      | UpdateMilestone             |
| DELETE            | `/api/repos/{owner}/{repo}/milestones/{number}`           | authMW | CanWrite (handler)      | DeleteMilestone             |
| POST              | `.../issues/{number}/milestone`                           | authMW | CanWrite (handler)      | SetIssueMilestone           |
| POST              | `.../pulls/{number}/milestone`                            | authMW | CanWrite (handler)      | SetPullMilestone            |
| POST/PATCH/DELETE | `/api/repos/{owner}/{repo}/releases`                      | authMW | CanWrite (handler)      | Release CRUD                |
| POST              | `/api/repos/{owner}/{repo}/statuses/{sha}`                | authMW | CanWrite (handler)      | CreateStatus                |
| POST/DELETE       | `/api/repos/{owner}/{repo}/branches`                      | authMW | CanWrite (handler)      | CreateBranch / DeleteBranch |
| POST/DELETE       | `/api/repos/{owner}/{repo}/tags`                          | authMW | CanWrite (handler)      | CreateTag / DeleteTag       |
| POST              | `/api/repos/{owner}/{repo}/wiki/{slug}`                   | authMW | CanWrite (handler)      | CreateOrUpdateWikiPage      |
| POST              | `/api/repos/{owner}/{repo}/discussions`                   | authMW | CanWrite (handler)      | CreateDiscussion            |
| POST              | `.../discussions/{number}/replies`                        | authMW | CanWrite (handler)      | CreateReply                 |
| PATCH             | `.../discussions/{number}`                                | authMW | CanWrite (handler)      | MarkAnswer                  |
| DELETE            | `.../discussions/{number}/replies/{id}`                   | authMW | CanWrite (handler)      | DeleteDiscussionReply       |
| POST              | `/api/repos/{owner}/{repo}/fork`                          | authMW | CanRead + authenticated | ForkRepo                    |
| POST              | `/api/repos/{owner}/{repo}/star`                          | authMW | User-specific action    | StarRepo                    |
| PUT/DELETE        | `/api/repos/{owner}/{repo}/watch`                         | authMW | User-specific action    | WatchRepo / UnwatchRepo     |
| POST              | `/api/repos/{owner}/{repo}/comments/{id}/reactions`       | authMW | CanRead + authenticated | ToggleReaction              |

### Repository Endpoints — Manage (Require CanManage or Owner)

| Method            | Path                                      | Auth   | AuthZ Check         | Handler                 |
| ----------------- | ----------------------------------------- | ------ | ------------------- | ----------------------- |
| PATCH             | `.../issues/{number}/pin`                 | authMW | CanManage (handler) | PinIssue                |
| PATCH             | `.../issues/{number}/lock`                | authMW | CanManage (handler) | LockIssue               |
| POST/PATCH/DELETE | `.../branches/protections`                | authMW | CanManage (handler) | BranchProtection CRUD   |
| POST/DELETE       | `.../hooks`                               | authMW | CanManage (handler) | Webhook CRUD            |
| POST/DELETE       | `/api/repos/{owner}/{repo}/collaborators` | authMW | CanManage (handler) | Collaborator CRUD       |
| POST/DELETE       | `/api/repos/{owner}/{repo}/keys`          | authMW | CanManage (handler) | DeployKey CRUD          |
| PUT               | `/api/repos/{owner}/{repo}/topics`        | authMW | CanManage (handler) | SetTopics               |
| POST              | `/api/repos/{owner}/{repo}/transfer`      | authMW | IsOwner (service)   | TransferRepo            |
| POST              | `/api/repos/{owner}/{repo}/archive`       | authMW | IsOwner (service)   | ArchiveRepo             |
| POST              | `/api/repos/{owner}/{repo}/unarchive`     | authMW | IsOwner (service)   | UnarchiveRepo           |
| POST              | `/api/repos/{owner}/{repo}/restore`       | authMW | IsOwner (service)   | RestoreRepo             |
| PATCH             | `/api/repos/{owner}/{repo}/template`      | authMW | IsOwner (service)   | SetRepoTemplate         |
| DELETE            | `/api/repos/{owner}/{repo}/wiki/{slug}`   | authMW | CanManage (handler) | DeleteWikiPage          |
| POST/DELETE       | `.../discussions/categories`              | authMW | CanManage (handler) | DiscussionCategory CRUD |

### Repository Endpoints — Service-Layer Auth (Project Board)

| Method | Path                                | Auth   | AuthZ Check         | Handler → Service |
| ------ | ----------------------------------- | ------ | ------------------- | ----------------- |
| POST   | `.../projects`                      | authMW | CanWrite (service)  | CreateProject     |
| DELETE | `.../projects/{id}`                 | authMW | CanManage (service) | DeleteProject     |
| POST   | `.../projects/{id}/columns`         | authMW | CanWrite (service)  | CreateColumn      |
| DELETE | `.../projects/{id}/columns/{colID}` | authMW | CanWrite (service)  | DeleteColumn      |
| POST   | `.../projects/{id}/cards`           | authMW | CanWrite (service)  | CreateCard        |
| PATCH  | `.../projects/{id}/cards/{cardID}`  | authMW | CanWrite (service)  | MoveCard          |
| DELETE | `.../projects/{id}/cards/{cardID}`  | authMW | CanWrite (service)  | DeleteCard        |

### Git Transport

| Method | Path                               | Auth            | AuthZ Check                | Handler        |
| ------ | ---------------------------------- | --------------- | -------------------------- | -------------- |
| GET    | `/{owner}/{repo}/info/refs`        | optAuthMW       | CanRead (handler)          | GitInfoRefs    |
| POST   | `/{owner}/{repo}/git-upload-pack`  | optAuthMW       | CanRead (handler)          | GitUploadPack  |
| POST   | `/{owner}/{repo}/git-receive-pack` | optAuthMW       | CanWrite (handler)         | GitReceivePack |
| SSH    | port 2222                          | Public key auth | CanRead/CanWrite (handler) | SSH server     |

### Notification Endpoints (Own Data Only)

| Method | Path                              | Auth   | AuthZ    | Handler                  |
| ------ | --------------------------------- | ------ | -------- | ------------------------ |
| POST   | `/api/notifications/read-all`     | authMW | Own user | MarkAllNotificationsRead |
| PATCH  | `/api/notifications/{id}`         | authMW | Own user | MarkNotificationRead     |
| GET    | `/api/notifications/unread-count` | authMW | Own user | GetUnreadCount           |

---

## Security Measures

| Measure            | Implementation                                                                   |
| ------------------ | -------------------------------------------------------------------------------- |
| CORS               | Origin restricted to `config.Server.BaseURL`; `localhost:3000` added in dev only |
| CSRF               | Double-submit cookie; token injected into HTMX `hx-headers` via layout template  |
| Body size limit    | 1 MB via `http.MaxBytesReader` on all `/api/*` routes                            |
| JWT secret warning | Log warning at startup if default secret is still set                            |
| Cookie security    | `Secure` flag configurable via `config.Auth.CookieSecure`; `HttpOnly` always set |
| Input validation   | All URL path params validated via `strconv`; repo/user names validated via regex |
| SSRF protection    | Webhook delivery blocks private/internal IPs                                     |
| Branch protection  | Ref rollback on protection violation after git-receive-pack                      |
| Password storage   | bcrypt hashed                                                                    |
| TOTP               | HMAC-SHA1 with bcrypt-hashed backup codes                                        |
| PAT                | `crypto/rand` generated, bcrypt-hashed for storage                               |
| SQL injection      | All queries use parameterized placeholders (`$1`, `$2`, ...)                     |

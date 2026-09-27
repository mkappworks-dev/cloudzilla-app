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
| OAuth App Token       | `Authorization: Bearer <token>` header             | Scoped API endpoints (below)  |
| SSH Public Key        | Key fingerprint lookup in `ssh_keys`/`deploy_keys` | Git SSH transport             |
| TOTP 2FA              | 6-digit code after password login                  | `POST /auth/2fa/verify`       |

### JWT Claims

All authenticated requests carry JWT claims in context:

```go
type Claims struct {
    UserID       int64
    Username     string
    IsSuperadmin bool
    Scoped       bool     // true only for OAuth-app tokens
    Scopes       []string // granted scopes when Scoped
}
```

Extracted via `middleware.ClaimsFromContext(r.Context())`. `claims.HasScope(s)` is always true for unscoped credentials (JWT sessions, PATs), which keep the user's full access. OAuth-app claims never carry `IsSuperadmin`.

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

- **authMW**: Reads JWT from `Authorization: Bearer` header OR `cz_token` httpOnly cookie. Also accepts PATs and OAuth tokens. Returns 401 if missing/invalid. An OAuth token without a scope for the route gets 403 (see [OAuth App Scopes](#oauth-app-scopes)).
- **optAuthMW**: Same as authMW but allows unauthenticated requests through. Claims may be nil. An OAuth token is still refused with 403 on routes its scopes don't cover — it is never silently downgraded to anonymous.
- **superadminMW**: Requires `claims.IsSuperadmin == true`. Returns 403 otherwise.
- **CSRF**: Double-submit cookie pattern. Skips git transport, Bearer-auth, `POST /oauth/token` (client-secret auth), and safe methods (GET/HEAD/OPTIONS).

### OAuth App Scopes

An OAuth-app token acts as the user who granted it, but only on routes its scopes admit. Enforcement is a path allow-list in `internal/middleware/scope.go`, applied by both `authMW` and `optAuthMW`: a route it doesn't list — including any new route — is closed to OAuth tokens. Scopes only narrow access; the handler's own `CanRead`/`CanWrite`/`CanManage` checks against the user still apply.

Open routes:

- `/api/repos` (list, create), `POST /api/repos/from-template`, `/api/repos/{owner}/{repo}` (read only — `PATCH` changes settings).
- Content sub-resources of `/api/repos/{owner}/{repo}`: `issues`, `pulls`, `labels`, `milestones`, `releases`, `statuses`, `commits`, `branches` (not `branches/protections`), `tags`, `comments`, `stargazers`, `star`, `watch`, `fork`, `projects`, `wiki`, `discussions`. A few of these `GET`s return HTML fragments (e.g. the watch button, issue title/body sections) carrying the same data as the JSON.
- `GET /api/orgs/{org}`, `GET /api/orgs/{org}/members`, `POST /api/orgs/{org}/repos`.
- `GET /api/users/{username}`, `GET /api/users/{username}/repos`.
- Git smart-HTTP: `info/refs`, `git-upload-pack`, `git-receive-pack`.

| Scope          | Admits on the open routes                                                                               |
| -------------- | ------------------------------------------------------------------------------------------------------- |
| `repo:read`    | `GET`/`HEAD`; git clone/fetch                                                                           |
| `repo:write`   | Everything, including creating repos, merging, applying suggestions, and git push                      |
| `issues:write` | Reads, plus writes under `.../issues/**`                                                                |
| `pulls:write`  | Reads, plus writes under `.../pulls/**` — except merging, enabling auto-merge, and applying a suggestion |

Merging and enabling auto-merge are requests to `PATCH .../pulls/{number}`, so `UpdatePull` checks `claims.HasScope(repo:write)` itself; applying a suggestion is refused by path. `UpdateComment`/`DeleteComment` require the comment to belong to the issue or pull request in the URL, so neither `issues:write` nor `pulls:write` reaches the other's comments.

Everything else is closed whatever the scopes, notably: HTML pages and `/fragments/*` (which is what keeps a user's email, shown on their own profile, away from apps); `/api/user/*` (SSH keys, PATs, TOTP, saved replies), `/api/oauth/*`, `/api/admin/*`, `/api/notifications/*`, `/api/gists`, `/api/markdown/preview`; repo administration (`hooks`, `collaborators`, `keys`, `topics`, `transfer`, `archive`, `unarchive`, `restore`, `delete`, `template`, branch protections, settings); and org administration.

A refused request gets `403` with `{"error":"insufficient_scope"}` and `WWW-Authenticate: Bearer error="insufficient_scope", scope="<narrowest scope that would admit it>"` (the `scope` attribute is omitted on closed routes). Unknown scopes are rejected at `/oauth/authorize` with `400`. PAT scopes are recorded but not yet enforced; PATs remain unscoped.

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

**Manage** includes: collaborator CRUD, branch protection, deploy keys, topics, wiki deletion, webhook CRUD, repo settings page access.

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

    repo, ok := h.readableRepoJSON(w, r, owner, repoName)  // 404 unless readable
    if !ok { return }
    if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {  // Layer 2: authz
        writeError(w, 403, "forbidden"); return
    }
    // ... proceed with mutation
}
```

Some handlers delegate authorization to the service layer (e.g., ProjectService checks `CanWrite` internally with the passed `userID`).

### Private Repos Look Missing

A 403 for a private repo, next to a 404 for a missing one, confirms that the private repo exists. So a caller who can't read a repo gets exactly the response a missing repo gets, and only readers ever see a 403:

- Signed-in HTML repo pages load the repo with `h.readableRepo`, which renders the missing repo's 404 page.
- Every `/api/repos/{owner}/{repo}/…` and `/fragments/{owner}/{repo}/…` route starts with `h.readableRepoJSON`, which answers `404 {"error":"repo not found"}` in both cases. It runs before sub-resource lookups, body validation, and any `CanWrite`/`CanManage`/`IsOwner` check, including checks made in a service. `TestRepoAPI_PrivateRepoNonReader_LooksLikeMissingRepo` walks the router and holds every such route to this.
- Project board routes also require the project to belong to the URL's repo (`h.projectIDInRepo`). The project services authorize against the project's own repo, so their 403 would otherwise confirm that another repo's project ID exists.
- Line comment update, delete and apply-suggestion only act on a comment in the URL's repo (and, for apply, the URL's pull request).
- `POST /api/repos/from-template` answers a private repo's ID with the same 404 as a missing ID, before checking that it is a template.
- `POST /api/repos/{owner}/{repo}/restore` targets a soft-deleted repo, which `readableRepoJSON` can't see. A caller who may not restore it gets the same 404 as when no deleted repo exists.

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
| POST                  | `/settings/email`                | authMW | Own user (claims.UserID)  | UpdateEmailSettings      |
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

`readableRepoJSON` answers a repo the caller can't read with the missing repo's 404 (see [Private Repos Look Missing](#private-repos-look-missing)).

| Method   | Path                                                                          | Auth      | AuthZ                          | Handler                           |
| -------- | ----------------------------------------------------------------------------- | --------- | ------------------------------ | --------------------------------- |
| GET      | `/api/repos`                                                                  | optAuthMW | Public list                    | ListRepos                         |
| GET      | `/api/repos/{owner}/{repo}`                                                   | optAuthMW | readableRepoJSON               | GetRepo                           |
| GET      | `/api/repos/{owner}/{repo}/issues`, `.../issues/{number}` (+ `/title`, `/body`, `/comments`) | optAuthMW | readableRepoJSON + issue visibility | ListIssues / GetIssue / … |
| GET      | `/api/repos/{owner}/{repo}/pulls`, `.../pulls/{number}` (+ `/reviews`, `/line_comments`) | optAuthMW | readableRepoJSON | ListPulls / GetPull / …     |
| GET      | `/api/repos/{owner}/{repo}/{labels,milestones,releases,stargazers,topics,watch}` (and sub-paths) | optAuthMW | readableRepoJSON | List/Get handlers   |
| GET      | `/api/repos/{owner}/{repo}/statuses/{sha}`, `.../commits/{sha}/status`        | optAuthMW | readableRepoJSON               | ListStatuses / GetCombinedStatus  |
| GET      | `/api/repos/{owner}/{repo}/comments/{id}/reactions`                           | optAuthMW | readableRepoJSON               | ListReactions                     |
| GET      | `/api/repos/{owner}/{repo}/{hooks,collaborators,keys,branches/protections}`   | optAuthMW | readableRepoJSON + CanManage   | ListWebhooks / ListCollaborators / ListDeployKeys / ListBranchProtections |
| GET      | `/fragments/{owner}/{repo}/issues/{number}/comments`                          | optAuthMW | readableRepoJSON               | IssueCommentsFragment             |
| GET      | `/{owner}/{repo}/{issues,pulls,releases,milestones}`                          | optAuthMW | CanRead                        | PageIssues / PagePulls / …        |
| GET      | `/{owner}/{repo}/tree/blob/blame/commits/commit`                              | optAuthMW | CanRead                        | Code browser                      |
| GET/POST | `/{owner}/{repo}/issues/new`                                                  | authMW    | readableRepo                   | PageNewIssue / PageNewIssueSubmit |
| GET/POST | `/{owner}/{repo}/pulls/new`                                                   | authMW    | readableRepo                   | PageNewPull / PageNewPullSubmit   |

### Repository Endpoints — Write (Require CanWrite)

Every row checks `readableRepoJSON` first.

| Method            | Path                                                      | Auth   | AuthZ Check                                  | Handler                     |
| ----------------- | --------------------------------------------------------- | ------ | -------------------------------------------- | --------------------------- |
| POST              | `/api/repos/{owner}/{repo}/issues`                        | authMW | readableRepoJSON (private issue: CanWrite in service) | CreateIssue        |
| PATCH             | `/api/repos/{owner}/{repo}/issues/{number}`               | authMW | CanWrite (handler)                           | UpdateIssue                 |
| PATCH             | `.../issues/{number}/{title,body}`, POST `.../priority`   | authMW | CanWrite (handler)                           | EditIssueTitle / EditIssueBody / SetIssuePriority |
| POST              | `/api/repos/{owner}/{repo}/issues/{number}/comments`      | authMW | readableRepoJSON (CanManage if locked)       | CreateIssueComment          |
| POST              | `/api/repos/{owner}/{repo}/pulls/{number}/comments`       | authMW | readableRepoJSON                             | CreatePullComment           |
| PATCH             | `.../{issues,pulls}/{number}/comments/{id}`               | authMW | Author only                                  | UpdateComment               |
| DELETE            | `.../{issues,pulls}/{number}/comments/{id}`               | authMW | Author OR CanWrite                           | DeleteComment               |
| POST              | `/api/repos/{owner}/{repo}/pulls`                         | authMW | readableRepoJSON (+ service)                 | CreatePull                  |
| PATCH             | `/api/repos/{owner}/{repo}/pulls/{number}`                | authMW | CanWrite (handler)                           | UpdatePull                  |
| POST              | `/api/repos/{owner}/{repo}/pulls/{number}/reviews`        | authMW | CanWrite (handler); not PR author (service)  | SubmitReview                |
| POST/DELETE       | `.../pulls/{number}/reviewers`                            | authMW | CanWrite (handler)                           | Add/RemovePullReviewer      |
| POST              | `.../pulls/{number}/line_comments`                        | authMW | readableRepoJSON                             | CreateLineComment           |
| PATCH             | `.../pulls/{number}/line_comments/{id}`                   | authMW | Author only; comment in this repo            | UpdateLineComment           |
| DELETE            | `.../pulls/{number}/line_comments/{id}`                   | authMW | Author OR CanWrite; comment in this repo     | DeleteLineComment           |
| POST              | `.../pulls/{number}/line_comments/{id}/apply`             | authMW | CanWrite (handler); suggestion on this PR    | ApplySuggestion             |
| POST/DELETE       | `.../{issues,pulls}/{number}/linked-*/{number}`           | authMW | CanWrite (handler)                           | Link/UnlinkIssuePull, Link/UnlinkPullIssue |
| POST              | `/api/repos/{owner}/{repo}/labels`                        | authMW | CanWrite (handler)                           | CreateLabel                 |
| DELETE            | `/api/repos/{owner}/{repo}/labels/{id}`                   | authMW | CanWrite (handler)                           | DeleteLabel                 |
| POST/DELETE       | `.../{issues,pulls,discussions}/{number}/labels/{labelID}` | authMW | CanWrite (handler)                          | Add/Remove*Label            |
| POST/DELETE       | `.../{issues,pulls}/{number}/assignees`                   | authMW | CanWrite (handler)                           | Add/Remove*Assignee         |
| POST              | `/api/repos/{owner}/{repo}/milestones`                    | authMW | CanWrite (handler)                           | CreateMilestone             |
| PATCH             | `/api/repos/{owner}/{repo}/milestones/{number}` (+ `/title`, `/body`, `/due`) | authMW | CanWrite (handler)       | UpdateMilestone / EditMilestone* |
| DELETE            | `/api/repos/{owner}/{repo}/milestones/{number}`           | authMW | CanWrite (handler)                           | DeleteMilestone             |
| POST              | `.../{issues,pulls}/{number}/milestone`                   | authMW | CanWrite (handler)                           | SetIssueMilestone / SetPullMilestone |
| POST/PATCH/DELETE | `/api/repos/{owner}/{repo}/releases` (and sub-paths)      | authMW | CanWrite (handler)                           | Release CRUD                |
| POST              | `/api/repos/{owner}/{repo}/statuses/{sha}`                | authMW | CanWrite (handler)                           | CreateStatus                |
| POST/DELETE       | `/api/repos/{owner}/{repo}/branches`                      | authMW | CanWrite (handler)                           | CreateBranch / DeleteBranch |
| POST/DELETE       | `/api/repos/{owner}/{repo}/tags`                          | authMW | CanWrite (handler)                           | CreateTag / DeleteTag       |
| POST              | `/api/repos/{owner}/{repo}/wiki/{slug}`, `.../wiki/order` | authMW | CanWrite (handler)                           | CreateOrUpdateWikiPage / WikiSetPageOrder |
| POST              | `/api/repos/{owner}/{repo}/discussions`                   | authMW | CanWrite (handler)                           | CreateDiscussion            |
| POST              | `.../discussions/{number}/replies`                        | authMW | readableRepoJSON                             | CreateReply                 |
| PATCH             | `.../discussions/{number}`                                | authMW | CanWrite (handler)                           | MarkAnswer                  |
| DELETE            | `.../discussions/{number}/replies/{id}`                   | authMW | CanWrite (handler)                           | DeleteDiscussionReply       |
| POST              | `/api/repos/{owner}/{repo}/fork`                          | authMW | readableRepoJSON                             | ForkRepo                    |
| POST              | `/api/repos/from-template`                                | authMW | Public, non-archived template (service)      | CreateFromTemplate          |
| POST/DELETE       | `/api/repos/{owner}/{repo}/star`                          | authMW | readableRepoJSON                             | StarRepo / UnstarRepo       |
| PUT/DELETE        | `/api/repos/{owner}/{repo}/watch`                         | authMW | readableRepoJSON                             | WatchRepo / UnwatchRepo     |
| POST              | `.../comments/{id}/reactions`, `.../discussions/{number}/reactions`, `.../replies/{id}/reactions` | authMW | readableRepoJSON | Toggle*Reaction |
| GET/POST          | `/{owner}/{repo}/discussions/new`                         | authMW | readableRepo + CanWrite                      | PageNewDiscussion / PageNewDiscussionSubmit |
| GET/POST          | `/{owner}/{repo}/milestones/new`                          | authMW | readableRepo + CanWrite                      | PageNewMilestone / PageNewMilestoneSubmit   |
| POST              | `/{owner}/{repo}/milestones/{number}`                     | authMW | readableRepo + CanWrite                      | PageMilestoneDetailAction   |
| GET               | `/{owner}/{repo}/releases/new`                            | authMW | readableRepo + CanWrite                      | PageReleaseNew              |
| GET/POST          | `/{owner}/{repo}/new/{ref}`                               | authMW | readableRepo + CanWrite                      | PageNewFile / SubmitNewFile |
| GET               | `/{owner}/{repo}/wiki/new`, `.../wiki/{slug}/edit`        | authMW | CanRead (404) + CanWrite                     | PageWikiNew / PageWikiEdit  |

### Repository Endpoints — Manage (Require CanManage or Owner)

Every `/api/repos` row checks `readableRepoJSON` first.

| Method            | Path                                      | Auth   | AuthZ Check                    | Handler                 |
| ----------------- | ----------------------------------------- | ------ | ------------------------------ | ----------------------- |
| PATCH             | `/api/repos/{owner}/{repo}`               | authMW | CanManage (service)            | UpdateRepo              |
| PATCH             | `.../issues/{number}/pin`                 | authMW | CanManage (service)            | PinIssue                |
| PATCH             | `.../issues/{number}/lock`                | authMW | CanManage (service)            | LockIssue               |
| POST/PATCH/DELETE | `.../branches/protections`                | authMW | CanManage (handler)            | BranchProtection CRUD   |
| POST/PATCH/DELETE | `.../hooks` (+ `/deliveries`, `/redeliver`) | authMW | CanManage (handler)          | Webhook CRUD            |
| POST/DELETE       | `/api/repos/{owner}/{repo}/collaborators` | authMW | CanManage (handler)            | Collaborator CRUD       |
| POST/DELETE       | `/api/repos/{owner}/{repo}/keys`          | authMW | CanManage (handler)            | DeployKey CRUD          |
| PUT               | `/api/repos/{owner}/{repo}/topics`        | authMW | CanManage (handler)            | SetTopics               |
| POST              | `/api/repos/{owner}/{repo}/transfer`      | authMW | IsOwner (handler)              | TransferRepo            |
| POST              | `/api/repos/{owner}/{repo}/archive`       | authMW | IsOwner (service)              | ArchiveRepo             |
| POST              | `/api/repos/{owner}/{repo}/unarchive`     | authMW | IsOwner (service)              | UnarchiveRepo           |
| POST              | `/api/repos/{owner}/{repo}/delete`        | authMW | IsOwner (service)              | DeleteRepo              |
| POST              | `/api/repos/{owner}/{repo}/restore`       | authMW | OwnerID + superadmin (service); 404 otherwise | RestoreRepo |
| PATCH             | `/api/repos/{owner}/{repo}/template`      | authMW | IsOwner (service)              | SetRepoTemplate         |
| DELETE            | `/api/repos/{owner}/{repo}/wiki/{slug}`   | authMW | CanManage (handler)            | DeleteWikiPage          |
| GET               | `/{owner}/{repo}/settings`                | authMW | readableRepo + CanManage       | PageRepoSettings        |
| POST              | `/{owner}/{repo}/settings/{general,features,visibility}` | authMW | readableRepo + CanManage (service) | UpdateRepoGeneral / Features / Visibility |

### Repository Endpoints — Service-Layer Auth (Project Board)

`CreateProject` checks `readableRepoJSON`; the other rows resolve the project through `projectIDInRepo` (readable repo that owns the project, else 404) before the service check.

| Method | Path                                | Auth   | AuthZ Check         | Handler → Service |
| ------ | ----------------------------------- | ------ | ------------------- | ----------------- |
| POST   | `.../projects`                      | authMW | CanWrite (service)  | CreateProject     |
| PATCH  | `.../projects/{id}`                 | authMW | CanWrite (service)  | UpdateProject     |
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

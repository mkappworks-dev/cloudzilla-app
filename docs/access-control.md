# Access Control — Authentication, Authorization, Roles & Permissions

## Authentication Methods

| Method                | Mechanism                                                                       | Where                         |
| --------------------- | ------------------------------------------------------------------------------- | ----------------------------- |
| Form login            | Email + password → JWT cookie (`cz_token`)                                      | `POST /login`                 |
| API login             | Email + password → JWT in cookie + JSON body; `401 totp_required` if TOTP is on | `POST /api/auth/login`        |
| Google OAuth          | OAuth 2.0 code flow → JWT cookie                                                | `GET /auth/google` → callback |
| LDAP                  | Bind + search → JWT cookie                                                      | `POST /auth/ldap`             |
| SAML SSO              | SP-initiated, ACS callback → JWT cookie                                         | `GET /auth/saml` → callback   |
| Personal Access Token | `Authorization: Bearer <token>` header; HTTP Basic password for git             | Scoped API endpoints (below)  |
| OAuth App Token       | `Authorization: Bearer <token>` header                                          | Scoped API endpoints (below)  |
| SSH Public Key        | Key fingerprint lookup in `ssh_keys`/`deploy_keys`                              | Git SSH transport             |
| TOTP 2FA              | 6-digit code after any web sign-in                                              | `POST /auth/2fa/verify`       |

Emails match case-insensitively everywhere: login, Google OAuth linking to an existing account by email, invites, and the existing-account check that makes LDAP and SAML refuse to auto-link. The `users_email_lower_key` index enforces it.

Usernames and organization names follow one rule and share one case-insensitive namespace; see [Usernames](#usernames).

Repository names (`service.ValidateName`) use letters, digits, `.`, `-` and `_` and start with a letter or digit. New repositories can't end in `.wiki`; see [api-reference](./api-reference.md) for repositories named that way before the reservation.

### Return path after sign-in

A signed-out HTML request is redirected to `/login?next=<request URI>`. Every
sign-in route redirects to `next` on success, once `safeNextPath` accepts it:
it must be a rooted same-site path, and it may not start with `//` or `/\` or
contain control characters. Anything else goes to `/`.

| Flow     | How `next` survives                                               |
| -------- | ----------------------------------------------------------------- |
| Password | Hidden `next` input on the login form                             |
| TOTP     | `/auth/2fa?next=…`, then a hidden input on the code form          |
| LDAP     | Hidden `next` input on the LDAP form                              |
| Google   | `oauth_next` cookie, set by `/auth/google?next=…`                 |
| SAML     | `RelayState`, dropped when it exceeds the binding's 80-byte limit |

### Two-factor authentication

TOTP is opt-in per user, from the Security tab of `/settings`. The password, LDAP, Google and SAML routes all end in `signIn` (`page_auth_handler.go`). For a user with TOTP on, it sets a five-minute `cz_totp_pending` cookie and redirects to `/auth/2fa` instead of issuing `cz_token`; `VerifyTOTP` starts the session once a code or backup code checks out. Every web session starts in `startSession`, which records the `login` audit event.

`VerifyTOTP` checks the code with `ReauthService.CheckSecondFactor`, which draws on the per-user failure limit of [confirmations](#confirming-sensitive-actions). After five wrong codes or backup codes in 15 minutes, the page refuses even the right code for the rest of the window, so a stolen password can't guess its way past 2FA.

Turning TOTP on needs the password as well as a code from the new authenticator: a code enrolled from a stolen session would lock the owner out at their next sign-in. Turning it off needs the password and a current code. Both mail the account a notice.

Google and SAML users get the prompt too, even when the IdP enforces its own MFA. Cloudzilla can't tell whether it did: it neither requests nor checks a SAML `AuthnContext`, and Google's userinfo doesn't say. Because TOTP is opt-in, only users who enrolled are asked, so a user whose IdP already handles MFA can leave it off. Gitea and GitLab make the same default, with a per-provider bypass that Cloudzilla doesn't have yet.

`POST /api/auth/login` has no second step. After the password and `AllowLogin` checks, it refuses a user with TOTP on with `401 {"error":"totp_required"}`: no `cz_token`, no token in the body, and no `login` event. API clients of such users authenticate with a personal access token instead. The endpoint accepts no TOTP code on purpose: a code field would add a second place to guess codes while giving nothing a PAT doesn't.

### Confirming sensitive actions

A session is a bearer credential, so anything that adds a way in that outlives it needs the account's own factors as well, whether the way in is for the session's holder or for someone they name. `ReauthService.Confirm` checks a `password` form field when the account has a password, and a `code` field (the TOTP code) when 2FA is on. An account with neither confirms with `email_code`, a code emailed to it. It applies to:

- changing the email address on `/settings/profile` (other profile fields save without it);
- changing the password, and turning 2FA on or off (see [Two-factor authentication](#two-factor-authentication) and [Changing the password](#changing-the-password));
- creating a personal access token (`POST /api/user/tokens`);
- adding an SSH key (`POST /api/user/keys`, form or JSON);
- approving an OAuth app on the consent page (`POST /oauth/authorize` with `action=approve`; denying needs nothing);
- connecting or disconnecting Google, which also refuse accounts without a password (`ConfirmWithPassword`);
- adding a repository collaborator, deploy key or webhook (form fields, or `password` and `code` in the webhook JSON body);
- transferring a repository or an organization;
- making someone an organization owner, by adding them as one or promoting a member. Adding a plain member and demoting an owner need nothing, since members get no repository until they're added to it. The owner check runs first, so a non-owner's request doesn't spend an attempt;
- making a private repository public (making one private needs nothing);
- deleting a repository, an organization or the account;
- superadmin changes: site settings, invitations, manual email verification, and the LDAP and SAML configuration.

Editing a webhook's events needs nothing: it can't point the webhook somewhere new.

**Accounts without a password or 2FA** (created by Google, LDAP or SAML sign-up) confirm with what they sign in with:

- An **LDAP** account types its directory password, checked by binding as the DN it signs in with.
- A **Google** or **SAML** account chooses **Confirm with Google** (or single sign-on): `POST /settings/reauth/{provider}` sends it to sign in there again, with `max_age=0` for Google and `ForceAuthn` for SAML. The state is single-use, lasts 5 minutes and is bound to the account (`oauth_states` purpose `reauth`, migration 096); for Google it must also come back to the browser that holds the `oauth_reauth_state` cookie. The callback checks that the provider signed in this same account (the Google ID, or the SAML user), never signs anyone in, and leaves a one-time code in an HttpOnly `cz_reauth` cookie. The next confirmed action in that browser uses it up; it lasts 10 minutes. The SAML response arrives as a cross-site POST without the session, so the code goes to whichever browser the IdP authenticated as the account.
- When the instance can send email, any of these accounts except LDAP can also choose **Email me a code** (`POST /settings/confirm-code`). Codes go out at most once a minute and five times an hour, and the email warns the owner that someone asked.

Codes, emailed or from a sign-in, are six digits, stored only as a bcrypt hash (`users.reauth_code_*`), work once, and count wrong guesses against the same failure limit. A stolen session can't read the mailbox or sign in as the owner at the provider. An account left with none of these, because the instance turned off its sign-in provider and has no email, is refused (`reauth_unavailable`) rather than let through. Accounts without a password are also kept out of Google linking by email (see [Google OAuth Sign-in](#google-oauth-sign-in)).

**Personal access tokens** are held to the scopes they were created with ([scopes](#oauth-app-scopes)). Only a token created with **`repo:admin`** reaches, and skips the confirmation for, for administering repositories and organizations: collaborators, deploy keys, webhooks, org owners, transfers and deletion (`confirmGrant`). OAuth apps can't be granted `repo:admin`, and it admits only those routes, never the account's own settings, so such a token can't add a password, key or token. Other tokens can't reach these routes at all, and a session JWT sent as a bearer token confirms like a browser.

A `repo:admin` token is **bound to a signing key**, given when it's created: an SSH public key, either Ed25519, ECDSA or RSA of 2048 bits or more, or a hardware key (`sk-ssh-ed25519`, `sk-ecdsa-sha2-nistp256`) that never leaves its security key. Every request with the token must be signed with the private key, so whoever copies the token string, from a log, a CI variable or a commit, can't use it. As in OpenSSH, a hardware key's signature must show it was touched, unless the key was given with the `no-touch-required` option (for unattended use). Creating a `repo:admin` token takes the password, an expiry within 90 days, and at least one **target**: a repository you manage (`owner/repo`) or an organization you own (`org`). Each use mails the owner the request it made. Any token can be bound to a key the same way; a bound token can't be used for git, which can't sign. See [signed requests](./api-reference.md#signed-requests) for the format.

A token's targets (`access_tokens.targets`, migration 098) limit every request it makes that names a repository or organization (`middleware.TargetAllows`): `/api/repos/{owner}/{repo}/…` must match a repository target or an organization target named `owner`, and `/api/orgs/{org}/…` must match an organization target. Anything else gets `403 {"error":"this token isn't allowed for that repository or organization"}`. An organization covers its settings and every repository it owns, including ones added later. Targets are checked to exist and to be the creator's to administer when the token is made; a repository that doesn't exist and one that isn't yours are refused alike, and a target that's renamed or transferred later no longer matches.

`authMW` and `optAuthMW` check a bound token's signature (`AccessTokenService.VerifySignedRequest`) before anything else uses it, and fail closed when the validator can't check one. The signature covers the method, path and query, a Unix timestamp within 5 minutes of the server's clock, a nonce, and the body's SHA-256. Each nonce is kept for 10 minutes (`access_token_nonces`, migration 097), so a captured request can't be sent again. An auth middleware that finds claims already set by an outer one passes the request on rather than checking it twice; routes under `optAuthMW` that add `authMW` would otherwise find their nonce spent.

What's left is the machine that holds the private key. Whoever controls it can sign, but only within the token's targets; a hardware key keeps the key from being copied off it, and its touch requirement keeps it from being used unattended. Keep a software key out of the repository, separate from the token, and revoke the token if that machine is compromised.

A wrong password or code gets `403`, or the form's own error: a `profile_error=reauth_failed` redirect on settings, or the error slot of a modal dialog (`HX-Retarget`), where a toast would sit behind the backdrop. Five failures within 15 minutes, counted per user across all of these actions and the `/auth/2fa` code page, block further attempts for the rest of the window with `429` (`reauth_throttled`). This stops a stolen session from guessing the password. The counts live on the user row (`reauth_failures`, `reauth_window_start`, migration 094), so every instance shares them. Each attempt is claimed with one conditional `UPDATE` before the password is checked, so concurrent guesses can't get past the limit, and a success gives its attempt back. The window runs from its first failure and doesn't slide.

### Changing the password

Account settings → Security → **Change password** (`POST /settings/password`: `password`, `code` with 2FA, `new_password`, `new_password_confirm`). `UserService.ChangePassword` checks the length (8 characters to 72 bytes) before the confirmation, so a typo doesn't spend an attempt, then confirms with `ConfirmWithPassword`. One `UPDATE` swaps the hash and bumps `session_version`, matching the hash that was confirmed: of two changes racing on the same old password, only one goes through. Every session ends ([Ending sessions](#ending-sessions)) and the current browser gets a fresh cookie, so someone else who knew the old password is signed out. The handler writes `user.password.change` to the audit log, and the account is mailed a notice.

Accounts without a password (created by Google, LDAP or SAML sign-up) can't set one here: nothing could confirm it, and a stolen session could otherwise add a password sign-in.

### Ending sessions

Session JWTs are stateless, so each carries the user's `session_version` (claim `sv`, migration 093) from when it was issued. With `middleware.WithSessionVersions`, which the router passes to `authMW` and `optAuthMW`, every JWT-authenticated request reads the user's current version: one primary-key lookup. The request is refused (`authMW`) or treated as signed out (`optAuthMW`) when the version has moved on or the user no longer exists. Tokens from before the claim existed read as version 0.

Account settings → Sessions → **Sign out other sessions** (`POST /settings/sessions/revoke`) bumps the version, sets a fresh cookie for the current browser, and writes `user.sessions.revoke` to the audit log. Changing the password bumps it too. Personal access tokens, SSH keys and OAuth app grants are not sessions; they are revoked in their own sections.

### JWT Claims

All authenticated requests carry JWT claims in context:

```go
type Claims struct {
    UserID       int64
    Username     string
    IsSuperadmin bool
    Scoped       bool     // true for PATs and OAuth-app tokens
    Scopes       []string // granted scopes when Scoped
}
```

Extracted via `middleware.ClaimsFromContext(r.Context())`. `claims.HasScope(s)` is always true for JWT sessions, which are unscoped and keep the user's full access. Token claims (PATs and OAuth-app tokens) never carry `IsSuperadmin`; a PAT's also set `PAT` and its `TokenName`.

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

- **authMW**: Reads JWT from `Authorization: Bearer` header OR `cz_token` httpOnly cookie. Also accepts PATs and OAuth tokens. Returns 401 if missing/invalid. A token without a scope for the route gets 403 (see [Token Scopes](#token-scopes)).
- **optAuthMW**: Same as authMW but allows unauthenticated requests through. Claims may be nil. A PAT or OAuth token is still refused with 403 on routes its scopes don't cover — it is never silently downgraded to anonymous.
- **superadminMW**: Requires `claims.IsSuperadmin == true`. Returns 403 otherwise.
- **CSRF**: Double-submit cookie pattern. Skips git transport, Bearer-auth, `POST /oauth/token` (client-secret auth), and safe methods (GET/HEAD/OPTIONS).

### Token Scopes

A PAT or OAuth-app token acts as its user, but only on routes its scopes admit. Enforcement is a path allow-list in `internal/middleware/scope.go`, applied by both `authMW` and `optAuthMW`: a route it doesn't list — including any new route — is closed to tokens. Scopes only narrow access; the handler's own `CanRead`/`CanWrite`/`CanManage` checks against the user still apply.

Open routes:

- `/api/repos` (list, create), `POST /api/repos/from-template`, `/api/repos/{owner}/{repo}` (read only — `PATCH` changes settings).
- Content sub-resources of `/api/repos/{owner}/{repo}`: `issues`, `pulls`, `labels`, `milestones`, `releases`, `statuses`, `commits`, `branches` (not `branches/protections`), `tags`, `comments`, `stargazers`, `star`, `watch`, `fork`, `projects`, `wiki`, `discussions`. A few of these `GET`s return HTML fragments (e.g. the watch button, issue title/body sections) carrying the same data as the JSON.
- `GET /api/orgs/{org}`, `GET /api/orgs/{org}/members`, `POST /api/orgs/{org}/repos`.
- `GET /api/users/{username}`, `GET /api/users/{username}/repos`.
- Git smart-HTTP: `info/refs`, `git-upload-pack`, `git-receive-pack`.

| Scope          | Admits on the open routes                                                                                |
| -------------- | -------------------------------------------------------------------------------------------------------- |
| `repo:read`    | `GET`/`HEAD`; git clone/fetch                                                                            |
| `repo:write`   | Everything, including creating repos, merging, applying suggestions, and git push                        |
| `issues:write` | Reads, plus writes under `.../issues/**`                                                                 |
| `pulls:write`  | Reads, plus writes under `.../pulls/**` — except merging, enabling auto-merge, and applying a suggestion |
| `repo:admin`   | Personal access tokens only: `collaborators`, `keys`, `hooks`, `transfer` and `delete` under a repository, and adding or promoting members, `transfer` and `delete` under `/api/orgs/{org}`. Nothing else |

The same allow-list holds personal access tokens created with scopes, on `authMW` and `optAuthMW`; git's Basic-auth path, which neither sees, checks the scopes itself (`middleware.PATAllowsGit`): fetching needs a read scope and pushing `repo:write`.

Merging and enabling auto-merge are requests to `PATCH .../pulls/{number}`, so `UpdatePull` checks `claims.HasScope(repo:write)` itself; applying a suggestion is refused by path. `UpdateComment`/`DeleteComment` require the comment to belong to the issue or pull request in the URL, so neither `issues:write` nor `pulls:write` reaches the other's comments.

Everything else is closed whatever the scopes, notably: HTML pages and `/fragments/*` (which is what keeps a user's email, shown on their own profile, away from apps); `/settings/*` form posts (including connecting or disconnecting Google and `/settings/email/resend-verification`), `/verify-email` and `/auth/google/callback`; `/api/user/*` (SSH keys, PATs, TOTP, saved replies), `/api/oauth/*`, `/api/admin/*`, `/api/notifications/*`, `/api/gists`, `/api/markdown/preview`; repo administration (`hooks`, `collaborators`, `keys`, `topics`, `transfer`, `archive`, `unarchive`, `restore`, `delete`, `template`, branch protections, settings); and org administration.

A refused request gets `403` with `{"error":"insufficient_scope"}` and `WWW-Authenticate: Bearer error="insufficient_scope", scope="<narrowest scope that would admit it>"` (the `scope` attribute is omitted on closed routes). Unknown scopes are rejected at `/oauth/authorize` with `400`.

#### Personal access tokens

PATs get the same open routes and scopes as OAuth-app tokens; being first-party doesn't widen them. Account, admin and repo or org administration stay session-only, so a PAT can't mint PATs, add SSH keys or turn off TOTP.

- `POST /api/user/tokens` needs at least one scope, all of them known; otherwise `400`.
- git sends a PAT as the HTTP Basic password, which `authMW` and `optAuthMW` never see, so `resolveGitUser` (`internal/handler/git_http.go`) checks it against the same policy. A refusal is a plain-text `403` naming the missing scope, not `401`: on a `401`, git's credential helper erases the stored token.
- Migration `090` gave every token without a known scope all four scopes, so tokens created without scopes kept git and content-API access but lost the routes above. Tokens that named scopes are held to them.

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

Org members do not get implicit access to private repos. They must be added as explicit collaborators (`reader`/`writer`/`admin`) on each repo. Creating an org repo grants nothing either; see [Organization Repo Ownership](#organization-repo-ownership).

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

**Transfer/Delete** (owner-only) includes: repo transfer, archive, unarchive, template toggle, soft-delete/restore. A transfer's `new_owner` names a user or an org, resolved user first like `/{owner}`; moving a repo into an org also requires owning that org.

### Organization Repo Ownership

For org repos, `org_id` points to the org and `owner_id` is `NULL`. `created_by` names whoever created the repo but grants nothing, so a creator who leaves the org or is demoted to member keeps only their explicit repo role, if any. Access is determined by `org_members` and those roles:

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

// IsOwner — a personal repo's owner, or an owner of an org repo's org (transfer, delete, archive)
func (s *RepoService) IsOwner(ctx, repo, userID int64) bool
```

`IsOwner` looks only at `org_members` for an org repo and only at `owner_id` for a personal one, and the other three build on it. Store queries that filter many repos by what the viewer can read (account issue and PR lists and counts, the activity feed, the attention inbox, repo search) use `readableBy` and `ownedBy` (`internal/store/repo_store.go`), the SQL forms of `CanRead` and `IsOwner`.

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
- Line comment update, delete and apply-suggestion only act on a comment on the URL's pull request (`h.lineCommentOnURLPull`). Line comment IDs are global, so otherwise write access to one repo would reach another's comments, and `/apply` would commit its suggestion content.
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
4. On submit: one transaction claims the invite (`accepted_at`) and creates the user with `is_invited = TRUE`, JWT cookie set → redirect `/`

`is_invited` users always bypass `allow_registration` and `allow_login` checks.

An invite is usable only while unaccepted, unexpired, and no account has its email (case-insensitive). Every other token, unknown ones included, gets the same generic "no longer valid" page without the invitation, because accepted invitations stay in the table and their email now belongs to a registered account.

## Self-Service Signup

With `smtp.host` set, `/register` asks only for an email and always answers "Check your inbox", mailing in the background:

- New address: a single-use link to `/register/complete/{token}` (24 hours) where the owner picks a username and password. The account starts with its email verified, since only the address's owner could open the link.
- Address with an account: a "you already have an account — sign in" email.

The response never reveals whether an address has an account. Links are stored as SHA-256 hashes in `signup_tokens` (one per address; a new request replaces it), and an address gets at most one email per 5 minutes. `POST /register` and `POST /register/complete/{token}` are also limited to 10 per client IP per 15 minutes, each route with its own budget. If sending fails, the address stays throttled for 5 minutes, so a retry within that window sends nothing. Closing `allow_registration` stops outstanding links.

Without SMTP, `/register` is the classic username/email/password form, which still reveals whether an email is registered.

## Google OAuth Sign-in

`UserService.AuthenticateOAuth` resolves a Google login in this order:

1. An account already linked to the Google ID (`oauth_provider`, `oauth_id`) signs in.
2. Otherwise Google must report the email as verified (`verified_email` from the userinfo endpoint). If it doesn't, the callback re-renders the login page with a 403 and nothing is linked or created (`ErrOAuthEmailUnverified`).
3. If an account has that email (compared case-insensitively), Google is linked to it only when both sides have proven the address. The link needs all of these:
   - The account's email is verified (`users.email_verified_at`; see [Email Verification](#email-verification)). An unverified local address may belong to whoever registered, was invited with, or edited their profile to it first, so the callback re-renders the login page with a 409 and links nothing (`ErrOAuthAccountExists`). The owner signs in with their password and then either verifies the address or [connects Google](#connecting-google-to-an-existing-account) explicitly.
   - The account isn't linked to a Google ID already. A second Google account for the same address gets a 409 (`ErrOAuthAlreadyLinked`) and the existing link stays.
   - `allow_login` admits the account, as for a linked sign-in (superadmin, invited, or login enabled); otherwise `ErrLoginDisabled` and nothing is linked.
   - The account has a password. Its email change is then [confirmed](#confirming-sensitive-actions), so a stolen session can't swap in an address it controls, verify it, and pull in its own Google account. Accounts created through LDAP or SAML have no password and get `ErrOAuthAccountExists`.

   The link is written only after every factor has passed. A link made before the TOTP prompt would outlast a failed or abandoned code, and could claim the account's one Google slot for whoever holds that Google account. Without TOTP, `signInLinking` links before starting the session. With TOTP, the pending link rides in the signed `cz_totp_pending` token, and `VerifyTOTP` links once the code checks out. `OAuthLinkService.LinkByVerifiedEmail` → `UserStore.LinkOAuthByVerifiedEmail` re-checks the email, its verification and the empty link in the `UPDATE` itself, so an email change or another link that lands in between wins. The sign-in then fails, because the Google identity was a first factor only by matching the address. Each link is audited as `user.oauth.connect` (with `"via": "verified_email"`) and emailed to the account, like an explicit connect.

4. Otherwise a new account is created, subject to `allow_registration`, linked to the Google ID, and starts with its email verified, since Google verified it.

LDAP and SAML still never link on email, verified or not: only the IdP operator vouches for the address they assert.

## Email Verification

`users.email_verified_at` (migration 091) is `NULL` until the address is proven. Accounts that existed before it start unverified. Google sign-ups and accounts created from a [signup link](#self-service-signup) start verified. Proving an address takes a single-use link sent to it, so it needs SMTP (`smtp.host`, see [configuration](./configuration.md#email-smtp)). Without SMTP, `EmailVerificationService.Available()` is false: no link is ever issued, Settings → Email shows a note instead of the resend button, and only a superadmin can mark an address verified.

**Links.** `<server.base_url>/verify-email?token=<token>`, where the token is 32 `crypto/rand` bytes, base64url-encoded. `email_verification_tokens` (migration 092) keeps only its SHA-256, with the user, the address it was sent to, `expires_at` (24 hours) and `used_at`. A link is issued when an invite is accepted (the admin supplied that address), on an email change, and when the user presses "Resend verification email" in Settings → Email (`POST /settings/email/resend-verification`). The classic registration form runs only without SMTP, so it issues none. The token is issued in the request, and the email goes out in the background so a slow SMTP server doesn't hold up the request. A delivery error is only logged and the token stays, since the server may have accepted the message before failing the session. The email and the confirm page name the account (username and address). An address's owner can then refuse to vouch for an account someone else registered with it, which would otherwise let that account take over their later Google sign-in. Settings → Email says whether a live link for the current address is out.

**Rate limit.** One link a minute per user and per address (case-insensitive), counted across resends, invite acceptance and email changes. It is checked under a lock on the user row and an advisory lock on the address, so concurrent requests can't both pass. Spent and revoked rows stay until the user's next link replaces them. A deleted user's rows keep counting for the address (`user_id` is set to `NULL`). So neither changing the address repeatedly, nor recreating the account, nor moving the address to another account buys another email to it. A new link revokes older ones. When an email change lands within the minute, no link goes out; Settings shows none is pending, and the user resends after the minute.

**Verifying.** `GET /verify-email` only checks the token and shows a confirm button, because mail scanners fetch links; `POST /verify-email` (CSRF-protected, no session needed) spends it. Both respond with `Referrer-Policy: no-referrer` and `Cache-Control: no-store`. A link verifies the account it was issued to, whoever submits it, and only while that account's email is still the address it was sent to. Only a pending link shows the account; otherwise the page reports verified, expired (410) or invalid (400). Invalid covers unknown, malformed, used and revoked links, and links for an address the account no longer has. Success spends all of the user's outstanding links and writes `user.email.verify` (method `link`) to the audit log.

**Email changes.** Changing the address needs the password and TOTP code ([Confirming sensitive actions](#confirming-sensitive-actions)). `UserService.UpdateProfile` clears `email_verified_at` and revokes outstanding links in the same transaction as the change. It then emails the old address that the change happened, naming the new one, and issues a link to the new address, subject to the rate limit. The handler writes `user.email.change` (from, to) to the audit log. Saving the profile with the same address needs no confirmation and keeps it verified.

**Manual verification.** A superadmin enters a username and that user's current email under `/admin/settings` → Email verification (`POST /api/admin/users/verify-email`). Both must match the account, the email case-insensitively (404 otherwise), so an address changed since the admin checked it is not vouched for. It writes `user.email.verify` (method `admin`, with the email) to the audit log, with the admin as actor.

A connected Google account is a sign-in that outlives the current session, so connecting one from settings needs the password and TOTP code, not just a session. A link made by verified email (step 3) needs no confirmation at sign-in, because both Google and the account proved the address. Every way to put a different address on the account is confirmed, and the link is emailed to the account and listed under Connected accounts, where it can be disconnected. Signing in with it still asks for the TOTP code (see [Two-factor authentication](#two-factor-authentication)).

## Connecting Google to an Existing Account

Account settings → Security → Connected accounts links a Google account to a signed-in account on request, instead of by matching emails. `OAuthLinkService` holds the rules; `ConnectGoogle`, `DisconnectGoogle` and the link mode of `GoogleOAuthCallback` call it.

**Connect** (`POST /settings/connected-accounts/google`, form `password` and `code`):

1. Refused unless `oauth.google_client_id` is set, and unless the password (plus the TOTP code, when TOTP is on; backup codes aren't accepted) is correct. A session alone is not enough, so a stolen session cannot plant a Google sign-in that outlives it. Accounts without a password (created by Google, LDAP or SAML sign-up) cannot re-authenticate, so they cannot connect.
2. `BeginLink` stores a random 32-byte state for 5 minutes in `oauth_states` (migration 088), bound to the user and the purpose `link`. Only its SHA-256 is stored, and a newer connect replaces the user's pending one. The raw state goes into the `oauth_link_state` cookie (HttpOnly, `Path=/auth/google/callback`) and into Google's authorization URL, which asks Google to let the user pick an account (`prompt=select_account`). The redirect is a 303, so the browser does not re-post the password to Google.

**Callback, link mode**: `GoogleOAuthCallback` runs link mode only when the `oauth_link_state` cookie matches the `state` parameter; otherwise it runs the login flow above, unchanged. The route runs `optAuthMW` so link mode can read the session. Link mode clears the cookie, then:

1. `ConsumeLinkState` deletes the state before anything else can refuse, so every attempt spends it, and returns the `LinkGrant` that `Link` requires: a replay, an expired state, a state started by another account, or one that reaches a signed-out browser is refused and cannot be retried.
2. The signed-in user must be the one who started the flow.
3. Google must report `verified_email`, and its userinfo must carry an `id`.
4. If that Google ID is linked to this account, nothing changes. If it is linked to another account, or this account is already linked to a different Google ID, the link is refused. The update only runs `WHERE oauth_provider = ''`, and `users_oauth_idx` refuses a Google ID another account took meanwhile.
5. Link mode never creates an account or issues a session cookie. Errors return to `/settings?profile_error=google_…#connected-accounts`; success shows a one-time notice through the `cz_settings_notice` cookie.

**Disconnect** (`POST /settings/connected-accounts/google/disconnect`, form `password` and `code`) needs the same re-authentication and clears `oauth_provider`/`oauth_id`. An account without a password is refused (`ErrReauthNoPassword`), and the store's update also requires a password, so the account's only sign-in is never removed.

Both changes write an audit entry (`user.oauth.connect` with the Google ID and email, `user.oauth.disconnect` with the Google ID) and, when SMTP is configured, email the account a notice. The notice ignores notification preferences, since muting it would hide a takeover. Both routes need CSRF like any cookie-authenticated form post, and are closed to OAuth-app tokens.

## Usernames

Usernames and organization names are repository path segments, so a new one must match `^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$` and not be a reserved route segment, compared case-insensitively: `activity admin api apps attention auth authorizations explore file-row fragments from-template ghost gists invitations invite issues latest login logout new notifications oauth organizations orgs pulls read-all register repos search settings setup stars static topic unread-count verify-email` (`service.ValidateOwnerName`, checked only on create; a router test fails if a top-level route segment is missing from the list). Setup, registration, signup, invites and `POST /api/orgs` reject anything else. Google OAuth derives the username from the display name (falling back to the email's local part, then `user`) by dropping other characters; LDAP/SAML replace other characters with `_`, falling back to `sso_user`.

Users and organizations share one namespace (`/{owner}` and `<repos_root>/<owner>/`), compared case-insensitively, so `Acme` can't be registered while org `acme` exists. Every user and organization insert checks this in the same statement (`ownerNameTakenCond` in `internal/store/user_store.go`) and refuses a taken name with `ErrUsernameTaken`, or `ErrOrgNameTaken` for `OrgService.Create`; Google OAuth moves on to the next numbered candidate (`2`, `3`, …) instead. Triggers from migration 080 also lock each exact name, so two concurrent creates of one name cannot both take it.

## Account Deletion

`POST /settings/delete-account` calls `UserService.DeleteUser`. In one transaction, `UserStore.DeleteWithOwnedRepos` deletes the user's personal repositories (soft-deleted ones included) and everything in them, drops the user's pending review requests, hands what the user wrote in repos they don't own, org repos included, to the [ghost user](#the-ghost-user), and deletes the user row, which cascades to gists, keys, tokens, stars, watches, reactions and activity. Org repos have no `owner_id`, so they stay with their org: their `created_by` becomes `NULL`, and a `deleted_by` naming the user passes to the ghost.

`DeleteUser` first refuses (`ErrSoleOrgOwner`, shown as `sole_org_owner`) while the user is the only owner of an organization, before touching any directory. The transaction checks again with every org the user belongs to locked, the lock that promoting, demoting or removing a member also takes. Around that transaction, `RepoService.DeleteWithOwner` handles the repo directories:

1. `DeleteWithOwner` renames each personal repo's `<name>.git` and `<name>.wiki.git` to `.deleted.<unix_ts>`.
2. If the transaction fails (for example `ErrOwnedReposChanged`, when a repo was created or restored after step 1), it renames them back.
3. Once the row is gone, it removes those directories and the copies of every personal repo the user had soft-deleted, which `PurgeExpired` can no longer find. A wiki that a soft delete from before wikis moved with their repo left at `<name>.wiki.git` goes too, unless another row still names it.

The freed username can then be registered or taken as an org name. Repo creation refuses any name whose directory still exists, so nothing the old account left on disk is ever served under the new owner.

### The ghost user

As on GitHub, a deleted account's issues, pull requests, comments, reviews, discussions and other contributions to repos it didn't own stay, credited to `ghost`. Migration 089 creates that user and an SQL function, `ghost_user_id()`, returning its ID; if an account named `ghost` already exists, the ghost takes the first free `ghost<n>` instead.

`ghostReassignments` in `internal/store/user_store.go` lists the columns the delete moves to the ghost: every reference to `users(id)` with no `ON DELETE` action, which would otherwise block it. Usernames copied into those rows (`author_name`, `actor_name`) are rewritten too, so whoever registers the freed name isn't credited. A migration that adds such a reference must give it an `ON DELETE` action or add it to the list; `TestGhostReassignments_CoverEveryUserFKWithoutDeleteAction` fails until it does.

- **Sign-in:** the ghost has no password, OAuth or SSO identity, token or key, and email lookups skip it. Notification email is off.
- **Lookups:** lookups by username, email or search prefix skip it (`notGhost`), so nobody can add it to an org, transfer a repo or org to it, add it as a collaborator, assign it, mention it or request its review. `/ghost` and `GET /api/users/ghost` return 404. Lookups by ID still load it for the content it holds.
- **Setup:** `IsSetupComplete` counts accounts without it (`UserStore.CountAccounts`), so a fresh install still starts at `/setup`.
- **Name:** `ghost` is a reserved owner name, and the row itself holds its name against case variants. A trigger refuses to delete the row.
- **Reviews:** submitted reviews pass to the ghost and stay on the pull request, but count toward neither merge gate. `CountApprovals`, used by branch protection and by the count the pull request page shows, skips them, and a ghost's `changes_requested` doesn't block merging, since nobody could withdraw it. The one-review-per-reviewer index excludes the ghost, so it can hold several reviews on one pull.
- **Invitations:** pending instance invitations the user sent stay valid, sent by `ghost`. Delete them from the admin page to revoke them.

---

## Full Endpoint Authorization Matrix

### Instance / Admin Endpoints

| Method   | Path                            | Auth                  | AuthZ           | Handler                         |
| -------- | ------------------------------- | --------------------- | --------------- | ------------------------------- |
| GET      | `/admin/settings`               | authMW + superadminMW | Superadmin only | PageAdminSettings               |
| GET      | `/admin/audit-log`              | authMW + superadminMW | Superadmin only | PageAuditLog                    |
| GET/POST | `/admin/sso`                    | authMW + superadminMW | Superadmin only | PageSSOSettings / SaveSSOConfig |
| POST     | `/api/admin/settings`           | authMW + superadminMW | Superadmin only | UpdateSiteSetting               |
| POST     | `/api/admin/invitations`        | authMW + superadminMW | Superadmin only | CreateInvitation                |
| DELETE   | `/api/admin/invitations/{id}`   | authMW + superadminMW | Superadmin only | DeleteInvitation                |
| POST     | `/api/admin/users/verify-email` | authMW + superadminMW | Superadmin only | AdminVerifyEmail                |

### Authentication Endpoints (No Auth Required)

| Method   | Path                    | Handler                                                               |
| -------- | ----------------------- | --------------------------------------------------------------------- |
| GET/POST | `/setup`                | PageSetup / PageSetupSubmit                                           |
| GET/POST | `/invite/{token}`       | PageInvite / PageInviteSubmit                                         |
| GET/POST | `/login`                | PageLogin / PageLoginSubmit                                           |
| POST     | `/api/auth/login`       | Login                                                                 |
| POST     | `/api/auth/logout`      | Logout                                                                |
| GET      | `/auth/google`          | GoogleOAuthBegin                                                      |
| GET      | `/auth/google/callback` | GoogleOAuthCallback                                                   |
| POST     | `/auth/ldap`            | LDAPLogin                                                             |
| GET/POST | `/auth/saml`            | InitiateSAML / SAMLCallback                                           |
| GET/POST | `/auth/2fa`             | PageTOTPVerify / VerifyTOTP                                           |
| GET/POST | `/verify-email`         | PageVerifyEmail / VerifyEmailSubmit (optAuthMW; the token authorizes) |

`/auth/google/callback` runs `optAuthMW`: its [link mode](#connecting-google-to-an-existing-account) needs the session.

### User-Scoped Endpoints (Own Data Only)

| Method                | Path                                             | Auth   | AuthZ                                                                          | Handler                    |
| --------------------- | ------------------------------------------------ | ------ | ------------------------------------------------------------------------------ | -------------------------- |
| GET                   | `/settings`                                      | authMW | Own user                                                                       | PageSettings               |
| POST                  | `/settings/profile`                              | authMW | Own user; password + TOTP code to change the email                             | UpdateProfile              |
| POST                  | `/settings/profile-readme`                       | authMW | Own user                                                                       | UpdateProfileReadme        |
| POST                  | `/settings/notifications`                        | authMW | Own user                                                                       | UpdateNotificationSettings |
| POST                  | `/settings/email`                                | authMW | Own user                                                                       | UpdateEmailSettings        |
| POST                  | `/settings/email/resend-verification`            | authMW | Own user; one link a minute                                                    | ResendVerificationEmail    |
| POST                  | `/settings/delete-account`                       | authMW | Own user                                                                       | DeleteAccount              |
| POST                  | `/settings/sessions/revoke`                      | authMW | Own user                                                                       | RevokeSessions             |
| POST                  | `/settings/password`                             | authMW | Own user; current password + TOTP code; refused without a password             | ChangePassword             |
| POST                  | `/settings/connected-accounts/google`            | authMW | Own user; password + TOTP code re-auth                                         | ConnectGoogle              |
| POST                  | `/settings/connected-accounts/google/disconnect` | authMW | Own user; password + TOTP code re-auth; refused without a password             | DisconnectGoogle           |
| POST                  | `/settings/security/setup`                       | authMW | Own user                                                                       | SetupTOTP                  |
| POST                  | `/api/user/totp/enable`                          | authMW | Own user (claims.UserID); password                                             | EnableTOTP                 |
| POST                  | `/api/user/totp/disable`                         | authMW | Own user (claims.UserID); password + TOTP code                                 | DisableTOTP                |
| GET/POST/DELETE       | `/api/user/keys`                                 | authMW | Own user (claims.UserID); POST needs password + TOTP code                      | SSH key CRUD               |
| POST/DELETE           | `/api/user/tokens`                               | authMW | Own user (claims.UserID); POST needs password + TOTP code                      | Token create/revoke        |
| GET/POST/PATCH/DELETE | `/api/user/replies`                              | authMW | Own user (claims.UserID)                                                       | Saved reply CRUD           |
| POST/DELETE           | `/api/users/{id}/pinned-repos/{repoID}`          | authMW | Own user (`{id}` = claims.UserID, else 403); POST needs repo read access (404) | PinRepo / UnpinRepo        |
| POST/DELETE           | `/api/oauth/apps`                                | authMW | Own user (claims.UserID)                                                       | OAuth app CRUD             |
| DELETE                | `/api/oauth/authorizations/{id}`                 | authMW | Own user (claims.UserID)                                                       | RevokeOAuthAuthorization   |
| POST/PATCH/DELETE     | `/api/gists`                                     | authMW | Own gist (service checks)                                                      | Gist CRUD                  |

### Organization Endpoints

| Method | Path                                      | Auth      | AuthZ                        | Handler               |
| ------ | ----------------------------------------- | --------- | ---------------------------- | --------------------- |
| GET    | `/api/orgs/{org}`                         | optAuthMW | Public                       | GetOrg                |
| GET    | `/api/orgs/{org}/members`                 | optAuthMW | Public                       | ListOrgMembers        |
| POST   | `/api/orgs`                               | authMW    | Any authenticated user       | CreateOrg             |
| POST   | `/api/orgs/{org}/members`                 | authMW    | Org owner (service); password + TOTP code to add an owner | AddOrgMember |
| DELETE | `/api/orgs/{org}/members/{username}`      | authMW    | Org owner, or self (service) | RemoveOrgMember       |
| POST   | `/api/orgs/{org}/members/{username}/role` | authMW    | Org owner (service); password + TOTP code to promote to owner | UpdateOrgMemberRole |
| POST   | `/api/orgs/{org}/repos`                   | authMW    | Org owner (service)          | CreateOrgRepo         |
| POST   | `/api/orgs/{org}/transfer`                | authMW    | Org owner (handler and service); password + TOTP code | TransferOrg |
| POST   | `/api/orgs/{org}/profile`                 | authMW    | Org owner (service)          | UpdateOrgProfile      |
| POST   | `/api/orgs/{org}/repo-defaults`           | authMW    | Org owner (service)          | UpdateOrgRepoDefaults |
| POST   | `/api/orgs/{org}/delete`                  | authMW    | Org owner (service)          | DeleteOrg             |

### Repository Endpoints — Read

`readableRepoJSON` answers a repo the caller can't read with the missing repo's 404 (see [Private Repos Look Missing](#private-repos-look-missing)).

| Method   | Path                                                                                             | Auth      | AuthZ                               | Handler                                                                   |
| -------- | ------------------------------------------------------------------------------------------------ | --------- | ----------------------------------- | ------------------------------------------------------------------------- |
| GET      | `/api/repos`                                                                                     | optAuthMW | Public list                         | ListRepos                                                                 |
| GET      | `/api/repos/{owner}/{repo}`                                                                      | optAuthMW | readableRepoJSON                    | GetRepo                                                                   |
| GET      | `/api/repos/{owner}/{repo}/issues`, `.../issues/{number}` (+ `/title`, `/body`, `/comments`)     | optAuthMW | readableRepoJSON + issue visibility | ListIssues / GetIssue / …                                                 |
| GET      | `/api/repos/{owner}/{repo}/pulls`, `.../pulls/{number}` (+ `/reviews`, `/line_comments`)         | optAuthMW | readableRepoJSON                    | ListPulls / GetPull / …                                                   |
| GET      | `/api/repos/{owner}/{repo}/{labels,milestones,releases,stargazers,topics,watch}` (and sub-paths) | optAuthMW | readableRepoJSON                    | List/Get handlers                                                         |
| GET      | `/api/repos/{owner}/{repo}/statuses/{sha}`, `.../commits/{sha}/status`                           | optAuthMW | readableRepoJSON                    | ListStatuses / GetCombinedStatus                                          |
| GET      | `/api/repos/{owner}/{repo}/comments/{id}/reactions`                                              | optAuthMW | readableRepoJSON                    | ListReactions                                                             |
| GET      | `/api/repos/{owner}/{repo}/{hooks,collaborators,keys,branches/protections}`                      | optAuthMW | readableRepoJSON + CanManage        | ListWebhooks / ListCollaborators / ListDeployKeys / ListBranchProtections |
| GET      | `/fragments/{owner}/{repo}/issues/{number}/comments`                                             | optAuthMW | readableRepoJSON                    | IssueCommentsFragment                                                     |
| GET      | `/{owner}/{repo}/{issues,pulls,releases,milestones}`                                             | optAuthMW | CanRead                             | PageIssues / PagePulls / …                                                |
| GET      | `/{owner}/{repo}/tree/blob/blame/commits/commit`                                                 | optAuthMW | CanRead                             | Code browser                                                              |
| GET/POST | `/{owner}/{repo}/issues/new`                                                                     | authMW    | readableRepo                        | PageNewIssue / PageNewIssueSubmit                                         |
| GET/POST | `/{owner}/{repo}/pulls/new`                                                                      | authMW    | readableRepo                        | PageNewPull / PageNewPullSubmit                                           |

### Repository Endpoints — Write (Require CanWrite)

Every row checks `readableRepoJSON` first.

| Method            | Path                                                                                              | Auth   | AuthZ Check                                           | Handler                                           |
| ----------------- | ------------------------------------------------------------------------------------------------- | ------ | ----------------------------------------------------- | ------------------------------------------------- |
| POST              | `/api/repos/{owner}/{repo}/issues`                                                                | authMW | readableRepoJSON (private issue: CanWrite in service) | CreateIssue                                       |
| PATCH             | `/api/repos/{owner}/{repo}/issues/{number}`                                                       | authMW | CanWrite (handler)                                    | UpdateIssue                                       |
| PATCH             | `.../issues/{number}/{title,body}`, POST `.../priority`                                           | authMW | CanWrite (handler)                                    | EditIssueTitle / EditIssueBody / SetIssuePriority |
| POST              | `/api/repos/{owner}/{repo}/issues/{number}/comments`                                              | authMW | readableRepoJSON (CanManage if locked)                | CreateIssueComment                                |
| POST              | `/api/repos/{owner}/{repo}/pulls/{number}/comments`                                               | authMW | readableRepoJSON                                      | CreatePullComment                                 |
| PATCH             | `.../{issues,pulls}/{number}/comments/{id}`                                                       | authMW | Author only                                           | UpdateComment                                     |
| DELETE            | `.../{issues,pulls}/{number}/comments/{id}`                                                       | authMW | Author OR CanWrite                                    | DeleteComment                                     |
| POST              | `/api/repos/{owner}/{repo}/pulls`                                                                 | authMW | readableRepoJSON (+ service)                          | CreatePull                                        |
| PATCH             | `/api/repos/{owner}/{repo}/pulls/{number}`                                                        | authMW | CanWrite (handler)                                    | UpdatePull                                        |
| POST              | `/api/repos/{owner}/{repo}/pulls/{number}/reviews`                                                | authMW | CanWrite (handler); not PR author (service)           | SubmitReview                                      |
| POST/DELETE       | `.../pulls/{number}/reviewers`                                                                    | authMW | CanWrite (handler)                                    | Add/RemovePullReviewer                            |
| POST              | `.../pulls/{number}/line_comments`                                                                | authMW | readableRepoJSON                                      | CreateLineComment                                 |
| PATCH             | `.../pulls/{number}/line_comments/{id}`                                                           | authMW | Author only; comment on this PR                       | UpdateLineComment                                 |
| DELETE            | `.../pulls/{number}/line_comments/{id}`                                                           | authMW | Author OR CanWrite; comment on this PR                | DeleteLineComment                                 |
| POST              | `.../pulls/{number}/line_comments/{id}/apply`                                                     | authMW | CanWrite (handler); suggestion on this PR             | ApplySuggestion                                   |
| POST/DELETE       | `.../{issues,pulls}/{number}/linked-*/{number}`                                                   | authMW | CanWrite (handler)                                    | Link/UnlinkIssuePull, Link/UnlinkPullIssue        |
| POST              | `/api/repos/{owner}/{repo}/labels`                                                                | authMW | CanWrite (handler)                                    | CreateLabel                                       |
| DELETE            | `/api/repos/{owner}/{repo}/labels/{id}`                                                           | authMW | CanWrite (handler)                                    | DeleteLabel                                       |
| POST/DELETE       | `.../{issues,pulls,discussions}/{number}/labels/{labelID}`                                        | authMW | CanWrite (handler)                                    | Add/Remove\*Label                                 |
| POST/DELETE       | `.../{issues,pulls}/{number}/assignees`                                                           | authMW | CanWrite (handler)                                    | Add/Remove\*Assignee                              |
| POST              | `/api/repos/{owner}/{repo}/milestones`                                                            | authMW | CanWrite (handler)                                    | CreateMilestone                                   |
| PATCH             | `/api/repos/{owner}/{repo}/milestones/{number}` (+ `/title`, `/body`, `/due`)                     | authMW | CanWrite (handler)                                    | UpdateMilestone / EditMilestone\*                 |
| DELETE            | `/api/repos/{owner}/{repo}/milestones/{number}`                                                   | authMW | CanWrite (handler)                                    | DeleteMilestone                                   |
| POST              | `.../{issues,pulls}/{number}/milestone`                                                           | authMW | CanWrite (handler)                                    | SetIssueMilestone / SetPullMilestone              |
| POST/PATCH/DELETE | `/api/repos/{owner}/{repo}/releases` (and sub-paths)                                              | authMW | CanWrite (handler)                                    | Release CRUD                                      |
| POST              | `/api/repos/{owner}/{repo}/statuses/{sha}`                                                        | authMW | CanWrite (handler)                                    | CreateStatus                                      |
| POST/DELETE       | `/api/repos/{owner}/{repo}/branches`                                                              | authMW | CanWrite (handler)                                    | CreateBranch / DeleteBranch                       |
| POST/DELETE       | `/api/repos/{owner}/{repo}/tags`                                                                  | authMW | CanWrite (handler)                                    | CreateTag / DeleteTag                             |
| POST              | `/api/repos/{owner}/{repo}/wiki/{slug}`, `.../wiki/order`                                         | authMW | CanWrite (handler)                                    | CreateOrUpdateWikiPage / WikiSetPageOrder         |
| POST              | `/api/repos/{owner}/{repo}/discussions`                                                           | authMW | CanWrite (handler)                                    | CreateDiscussion                                  |
| POST              | `.../discussions/{number}/replies`                                                                | authMW | readableRepoJSON                                      | CreateReply                                       |
| PATCH             | `.../discussions/{number}`                                                                        | authMW | CanWrite (handler)                                    | MarkAnswer                                        |
| DELETE            | `.../discussions/{number}/replies/{id}`                                                           | authMW | CanWrite (handler)                                    | DeleteDiscussionReply                             |
| POST              | `/api/repos/{owner}/{repo}/fork`                                                                  | authMW | readableRepoJSON                                      | ForkRepo                                          |
| POST              | `/api/repos/from-template`                                                                        | authMW | Public, non-archived template (service)               | CreateFromTemplate                                |
| POST/DELETE       | `/api/repos/{owner}/{repo}/star`                                                                  | authMW | readableRepoJSON                                      | StarRepo / UnstarRepo                             |
| PUT/DELETE        | `/api/repos/{owner}/{repo}/watch`                                                                 | authMW | readableRepoJSON                                      | WatchRepo / UnwatchRepo                           |
| POST              | `.../comments/{id}/reactions`, `.../discussions/{number}/reactions`, `.../replies/{id}/reactions` | authMW | readableRepoJSON                                      | Toggle\*Reaction                                  |
| GET/POST          | `/{owner}/{repo}/discussions/new`                                                                 | authMW | readableRepo + CanWrite                               | PageNewDiscussion / PageNewDiscussionSubmit       |
| GET/POST          | `/{owner}/{repo}/milestones/new`                                                                  | authMW | readableRepo + CanWrite                               | PageNewMilestone / PageNewMilestoneSubmit         |
| POST              | `/{owner}/{repo}/milestones/{number}`                                                             | authMW | readableRepo + CanWrite                               | PageMilestoneDetailAction                         |
| GET               | `/{owner}/{repo}/releases/new`                                                                    | authMW | readableRepo + CanWrite                               | PageReleaseNew                                    |
| GET/POST          | `/{owner}/{repo}/new/{ref}`                                                                       | authMW | readableRepo + CanWrite                               | PageNewFile / SubmitNewFile                       |
| GET               | `/{owner}/{repo}/wiki/new`, `.../wiki/{slug}/edit`                                                | authMW | CanRead (404) + CanWrite                              | PageWikiNew / PageWikiEdit                        |

### Repository Endpoints — Manage (Require CanManage or Owner)

Every `/api/repos` row checks `readableRepoJSON` first.

| Method            | Path                                                     | Auth   | AuthZ Check                                   | Handler                                   |
| ----------------- | -------------------------------------------------------- | ------ | --------------------------------------------- | ----------------------------------------- |
| PATCH             | `/api/repos/{owner}/{repo}`                              | authMW | CanManage (service)                           | UpdateRepo                                |
| PATCH             | `.../issues/{number}/pin`                                | authMW | CanManage (service)                           | PinIssue                                  |
| PATCH             | `.../issues/{number}/lock`                               | authMW | CanManage (service)                           | LockIssue                                 |
| POST/PATCH/DELETE | `.../branches/protections`                               | authMW | CanManage (handler)                           | BranchProtection CRUD                     |
| POST/PATCH/DELETE | `.../hooks` (+ `/deliveries`, `/redeliver`)              | authMW | CanManage (handler); POST needs password + TOTP code | Webhook CRUD                       |
| POST/DELETE       | `/api/repos/{owner}/{repo}/collaborators`                | authMW | CanManage (handler); POST needs password + TOTP code | Collaborator CRUD                  |
| POST/DELETE       | `/api/repos/{owner}/{repo}/keys`                         | authMW | CanManage (handler); POST needs password + TOTP code | DeployKey CRUD                     |
| PUT               | `/api/repos/{owner}/{repo}/topics`                       | authMW | CanManage (handler)                           | SetTopics                                 |
| POST              | `/api/repos/{owner}/{repo}/transfer`                     | authMW | IsOwner (handler); password + TOTP code       | TransferRepo                              |
| POST              | `/api/repos/{owner}/{repo}/archive`                      | authMW | IsOwner (service)                             | ArchiveRepo                               |
| POST              | `/api/repos/{owner}/{repo}/unarchive`                    | authMW | IsOwner (service)                             | UnarchiveRepo                             |
| POST              | `/api/repos/{owner}/{repo}/delete`                       | authMW | IsOwner (service)                             | DeleteRepo                                |
| POST              | `/api/repos/{owner}/{repo}/restore`                      | authMW | OwnerID + superadmin (service); 404 otherwise | RestoreRepo                               |
| PATCH             | `/api/repos/{owner}/{repo}/template`                     | authMW | IsOwner (service)                             | SetRepoTemplate                           |
| DELETE            | `/api/repos/{owner}/{repo}/wiki/{slug}`                  | authMW | CanManage (handler)                           | DeleteWikiPage                            |
| GET               | `/{owner}/{repo}/settings`                               | authMW | readableRepo + CanManage                      | PageRepoSettings                          |
| POST              | `/{owner}/{repo}/settings/{general,features,visibility}` | authMW | readableRepo + CanManage (service)            | UpdateRepoGeneral / Features / Visibility |

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
| Branch protection  | A push that violates a rule is refused per ref, before the ref is written        |
| Password storage   | bcrypt hashed                                                                    |
| TOTP               | HMAC-SHA1 with bcrypt-hashed backup codes                                        |
| PAT                | `crypto/rand` generated, SHA-256-hashed for storage, limited to its scopes       |
| Email verification | 32 `crypto/rand` bytes, SHA-256 at rest, single use, 24 h, one link a minute     |
| SQL injection      | All queries use parameterized placeholders (`$1`, `$2`, ...)                     |

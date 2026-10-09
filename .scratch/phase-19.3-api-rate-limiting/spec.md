# API rate limiting and quotas

Created: 2026-10-06
Category: enhancement
Status: done

Roadmap: Phase 19.3 (API Rate Limiting & Quotas). Facts below were checked against `origin/main` at `4d1e48d7` on 2026-10-06.

The work ships as two PRs, in order:

- [`issues/01-rate-limits.md`](./issues/01-rate-limits.md)
- [`issues/02-quotas.md`](./issues/02-quotas.md)

The decisions are recorded under [Decisions](#decisions).

## Problem

### Rate limits

Only six routes have a rate limit, and nothing else does. One script, crawler or CI loop can use as much of the server as it likes.

`middleware.RateLimit(limit, window)` (`internal/middleware/rate_limit.go`) counts requests per client IPv4 address or IPv6 /64 in a fixed window held in process memory. Each call builds its own limiter, so each route has its own budget. On a refusal it answers `429` with `Retry-After` and a plain-text body. It sends no `X-RateLimit-*` headers. `internal/router/router.go` applies it only to:

- `POST /invite/{token}`, `/register` and `/register/complete/{token}`: 10 per 15 minutes
- `POST /login`, `/auth/ldap` and `/api/auth/login`: 30 per 15 minutes

Everything else is unlimited, including the expensive routes:

- every other `/api/*` route
- git over HTTP: `GET /{owner}/{repo}/info/refs`, `POST …/git-upload-pack` and `POST …/git-receive-pack`. go-git builds a whole pack for every clone or fetch
- `GET /search` and `GET /search/code`, both plain form submits rather than per-keystroke requests
- `GET /{owner}/{repo}/archive/{ref}`, which zips the whole tree at the ref
- `GET /{owner}/{repo}/raw/{ref}/*`
- `POST /api/markdown/preview`
- `POST /oauth/token`, `POST /auth/saml/callback` and `POST /auth/2fa/verify`

For example, an anonymous loop over `GET /acme/app/archive/main` zips the repository again on every request, and nothing slows it down.

### Quotas

Nothing limits how many repositories an owner creates or how much disk they use.

- No repository size is recorded: there is no column, no computed size and no UI.
- Pushes write loose objects, so disk use only shrinks when an operator runs `cloudzilla gc` (`cmd/cloudzilla/gc.go`, `gitgc.Prune`).
- The only caps are per request:
  - `git.max_pack_bytes`: 2 GiB per push, after decompression
  - the web file form: 25 MB
  - gists: 10 files of 1 MB each
  - imports: 5 active per user and 3 running at once

### What already exists

- **Client IP behind a proxy.** `middleware.ClientIP` runs globally, right after `RequestID`. It rewrites `r.RemoteAddr` from `X-Forwarded-For`, read right to left, but only when the peer is in `server.trusted_proxies` (`CZ_SERVER_TRUSTED_PROXIES`). `docs/deployment.md` warns that without the setting, every client behind a proxy shares the proxy's budget. Nothing at runtime notices the mistake.
- **2FA guessing.** `/auth/2fa/verify` has no IP limit, and it needs none.
  - `ReauthService.CheckSecondFactor` claims an attempt from a per-user window before checking the code: 5 failures per 15 minutes.
  - The window is stored on the user row, so every instance shares it (`TestReauthService_CheckSecondFactor_IsThrottled`).
  - An attacker reaches the page only with a `cz_totp_pending` cookie, and that is issued only after the first factor succeeds.
  - The anonymous `core` budget below caps request volume.
- **Where identity is resolved.** `Auth` and `OptionalAuth` are per-route (`r.With(authMW)`, or `r.Use` inside a route group). chi runs global middleware before routing, so a global limiter can't read `ClaimsFromContext`. Git over HTTP resolves a PAT sent as the Basic-auth password inside the handler (`resolveGitUser`), not in middleware. So even a per-route limiter would see `git clone` with a token as anonymous.
- **Cost of checking a credential.** A session JWT is checked with HMAC only, with no database query. A PAT (`czp_…`) or an OAuth-app token needs a database lookup (`AccessTokenService.Validate`, `OAuthAppService.ResolveOAuthToken`).
- **Where repositories are created.** Every new-repository path calls `claimRepo` (`internal/service/repo_dirs.go`):
  - `RepoService.Create`, `Fork` and `CreateFromTemplate`
  - `RepoService.CreateFromImport`
  - `OrgService.CreateRepo`

  Transfers (`moveRepo` in `repo_transfer.go`) and `RepoService.Restore` bring a repository to an owner without calling it.
- **Where repositories are written.**
  - Pushes go through `gittransport.NewLimitedReadCloser(body, MaxPackBytes)`. HTTP does this in `git_http.go` and answers 413 when the cap is crossed. SSH does it in `ssh/server.go` and writes the error to stderr.
  - Web commits go through `CodeService.CommitFile`.
  - Wiki edits go through `CodeService.WikiPage*` into `<owner>/<repo>.wiki.git`.
- **Ownership.** A repository has either `owner_id` (a user) or `org_id`, never both (the `repositories_owner_or_org` CHECK). `RepoStore.CountForUser` counts a user's live repositories; there is no org equivalent.
- **Dependencies.** `golang.org/x/time` isn't in `go.mod`.

### Where the roadmap entry is out of date

- It names migration `060_create_rate_limit_config.sql`, but `main` is already at `101_user_code_themes.sql`.
- It says unauthenticated requests "share a global bucket". Then a single scraper would lock every anonymous visitor out.
- The title says "& Quotas", but the body defines no quota.
- It puts limits in an admin panel, backed by a `rate_limit_config` table.

## Design: rate limits (ticket 01)

### One global limiter

Add a new middleware, `middleware.APIRateLimit(…)`. It is registered with `r.Use` after `CORS` and before `MaxFormBodySize`/`CSRF`:

- after `ClientIP`, so it sees the real client
- after `Logger`, so refusals are logged
- after `CORS`, which answers preflight requests itself, so they are never counted
- before the form parse, so a refused request isn't read

The existing `middleware.RateLimit(limit, window)` keeps its signature and its per-route budgets for login and sign-up, since the password-reset work uses it too. Those budgets stack on top of the global limiter.

### Who a request counts against

The limiter resolves the request's subject itself, because auth hasn't run yet:

| Credential | Bucket key |
| --- | --- |
| Session JWT (cookie or `Bearer`) with a valid signature | `user:<id>:web` |
| PAT (`Bearer czp_…`, or the Basic-auth password for git) that validates and isn't bound to a signing key | `user:<id>:token` |
| OAuth-app token that resolves | `user:<id>:token` |
| None, or one that doesn't verify | `ip:<IPv4>` or `ip:<IPv6 /64>` |

- A credential that doesn't verify counts against the IP. Random tokens can't buy fresh buckets.
- A PAT bound to a signing key also counts against the IP: only auth can check the request's signature, and a leaked token mustn't spend its owner's budget.
- The key only picks a bucket; it never grants access. That is why the limiter skips the session-version check: a revoked but unexpired JWT is still signed by this server, so it can't be forged. Auth refuses it later.
- A PAT or OAuth token that the limiter validated is stored in the request context. `servePAT`, `serveOAuth` and `resolveGitUser` use it instead of querying the database again.
- Keeping browser sessions and tokens in separate buckets means a runaway CI token can't lock its owner out of the web UI. All of a user's tokens share one bucket, so minting more tokens doesn't raise the budget.

### Resources

Each request is charged to exactly one resource. Each resource has its own budget per subject:

| Resource | Requests |
| --- | --- |
| `git` | `GET …/info/refs`, `POST …/git-upload-pack` and `POST …/git-receive-pack`. A clone, fetch or push is 2 requests |
| `archive` | `GET /{owner}/{repo}/archive/…` |
| `search` | `GET /search` and `GET /search/code` |
| `core` | everything else that isn't exempt, including blame, contributors and pulse |

Classification runs before routing, so it matches the path's shape:

- The `archive` rule excludes `/api/…`, so `POST /api/repos/{o}/{r}/archive`, which archives the repository, stays in `core`.
- A table test pins down the classifier.

### Exempt

These requests are not counted and get no rate-limit headers:

- `/static/*`, `/htmx.min.js`, `/alpine.min.js` and `/favicon.ico`
- the operator endpoints from the parallel health/metrics work. That work plans `/healthz`, `/readyz` and `/metrics`, and none exists on `main` yet. Whichever PR merges second adds them to the exempt list.

### Algorithm

The existing limiter already uses a fixed window per key and sweeps expired windows. Generalise it rather than add `golang.org/x/time/rate`. A fixed window gives exact values for `X-RateLimit-Remaining` and `X-RateLimit-Reset`; a token bucket has no reset time to report.

Accepted cost: a client can spend up to twice its budget across a window boundary.

### Responses

Every counted response carries GitHub's header names, which existing API clients already read:

- `X-RateLimit-Limit`
- `X-RateLimit-Remaining`
- `X-RateLimit-Reset` (Unix seconds)
- `X-RateLimit-Resource` (`core`, `git`, `archive` or `search`)

A refusal is `429` with `Retry-After` (seconds until reset):

- `/api/*` gets `{"error":"rate limit exceeded"}`, matching `writeError`.
- Git gets a plain-text body. Git shows a refused `info/refs` body to the user as `remote: …`.
- Pages get plain text, as the existing limiter sends.

The body comes from an `onLimited http.HandlerFunc` that the router passes in, the same way `Auth` takes `onUnauthorized`. That keeps rendering in the handler layer.

### Configuration

| Key | Default | Env |
| --- | --- | --- |
| `rate_limit.enabled` | `true` | `CZ_RATE_LIMIT_ENABLED` |
| `rate_limit.window` | `1h` | `CZ_RATE_LIMIT_WINDOW` |
| `rate_limit.core.authenticated` | `5000` | `CZ_RATE_LIMIT_CORE_AUTHENTICATED` |
| `rate_limit.core.anonymous` | `1000` | `CZ_RATE_LIMIT_CORE_ANONYMOUS` |
| `rate_limit.git.authenticated` | `1000` | `CZ_RATE_LIMIT_GIT_AUTHENTICATED` |
| `rate_limit.git.anonymous` | `200` | `CZ_RATE_LIMIT_GIT_ANONYMOUS` |
| `rate_limit.archive.authenticated` | `100` | `CZ_RATE_LIMIT_ARCHIVE_AUTHENTICATED` |
| `rate_limit.archive.anonymous` | `20` | `CZ_RATE_LIMIT_ARCHIVE_ANONYMOUS` |
| `rate_limit.search.authenticated` | `600` | `CZ_RATE_LIMIT_SEARCH_AUTHENTICATED` |
| `rate_limit.search.anonymous` | `60` | `CZ_RATE_LIMIT_SEARCH_ANONYMOUS` |

- `0` means unlimited for that class. A negative value is a startup error.
- The `authenticated` budget applies to each bucket separately: the `web` bucket and the `token` bucket each get the full number.

### Proxy misconfiguration warning

Suppose a request arrives with `X-Forwarded-For` from a peer that isn't in `server.trusted_proxies`. `ClientIP` then logs one warning per process, naming the peer and the setting. Without the setting, every anonymous client behind a proxy shares one anonymous budget.

### Several instances

Counts live in each process. Behind a round-robin load balancer, N instances allow up to N times each budget.

The counter is one small type (`fixedWindow.take(key, limit) → remaining, reset, ok`). Phase 20.2 can put a Postgres-backed one with the same method behind an interface. Doing that now would add a database write to every request, page views included, before anything runs more than one instance.

`docs/deployment.md` states the limitation.

## Design: quotas (ticket 02)

### Limits

Four instance-wide limits, all off by default:

| Key | Default | Env |
| --- | --- | --- |
| `quota.user.repos` | `0` | `CZ_QUOTA_USER_REPOS` |
| `quota.user.storage_bytes` | `0` | `CZ_QUOTA_USER_STORAGE_BYTES` |
| `quota.org.repos` | `0` | `CZ_QUOTA_ORG_REPOS` |
| `quota.org.storage_bytes` | `0` | `CZ_QUOTA_ORG_STORAGE_BYTES` |

- `0` means unlimited, so upgrading changes nothing until an operator sets a value. A negative value is a startup error.
- Every user gets the same quota, and so does every org; there are no per-owner overrides.
- A superadmin's personal account is exempt. Orgs are always subject to their quota.

### Usage

- **Count.** An owner's live repositories, those with `deleted_at IS NULL`, by `owner_id` or `org_id`.
- **Storage.** The sum of `repositories.size_bytes` over the owner's live repositories.
- A repository's size is the on-disk size of `<owner>/<name>.git` plus `<owner>/<name>.wiki.git`.
- Soft-deleted repositories don't count, so deleting one frees quota at once.
- Gists are excluded: they live in the database and are already capped.

### Measuring size

- **Migration.** Use the next free number at commit time (`102` today, but parallel branches may take it). It adds `repositories.size_bytes BIGINT`. `NULL` means not yet measured, and an unmeasured repository counts as 0.
- **Backfill.** A background sweep at startup measures every repository whose size is `NULL`.
- **Recompute.** After each write, a background recompute walks the repository's directories and stores the total. Recomputes of one repository coalesce. They run after:
  - a push on either transport
  - `CommitFile`
  - a wiki write
  - an import
  - a fork or a template creation
  - `cloudzilla gc`
  - the web file edit and mirror sync paths, which parallel sessions are adding; whichever lands second wires them in
- **Staleness.** Usage can lag a write by one recompute. That is accepted.

### Enforcement

- **Repository count.** A new `QuotaService` checks the target owner before every `claimRepo` call, before a transfer is offered and again when it is accepted, and before `Restore`.
  - `ImportService.Start` checks it too, so the user hears at once rather than when the background job fails.
  - The parallel mirrors work adds another creation path, and it must check as well.
  - A refusal is `403` with `{"error":"repository quota reached (50 of 50)"}`, or the form's own error on pages.
- **Storage on push.** Each push is capped at `min(git.max_pack_bytes, quota − usage)` and refused like an oversized pack: 413 over HTTP, stderr over SSH, with a message stating usage and quota.
  - When nothing is left, any pack data is refused, but a push that only deletes refs still goes through.
  - `LimitedReadCloser` treats a cap of `0` or less as "no cap", so zero remaining needs explicit handling.
- **Storage on web writes.** `CommitFile` and wiki writes are refused when the owner is at or over quota.

### UI

When a quota is set, user Settings and org settings show one line, for example `Repositories 12 of 50 · Storage 1.2 GB of 10 GB`. The line names only the limits that are set.

## Acceptance criteria

### 01: rate limits

- [x] Anonymous requests to a counted route share a budget per IPv4 address or IPv6 /64. Over budget they get `429` with `Retry-After`.
- [x] A session request and a PAT request from the same user count against separate buckets. Two PATs of one user share a bucket.
- [x] `git clone` with a PAT as the Basic-auth password counts against the user's `token` bucket, not the IP.
- [x] A request carrying an invalid token counts against its IP.
- [x] A PAT is validated once per request, not once in the limiter and again in auth.
- [x] `git`, `archive` and `search` requests use their own budgets and leave `core` untouched. `POST /api/repos/{o}/{r}/archive` counts as `core`.
- [x] Counted responses carry `X-RateLimit-Limit`, `-Remaining`, `-Reset` and `-Resource`. Exempt paths carry none and are never refused.
- [x] A refused `/api/*` request gets a JSON error body, and git gets a plain-text one.
- [x] Every `rate_limit.*` key loads from YAML and from its `CZ_*` variable. `0` disables that class, a negative value fails startup, and `rate_limit.enabled: false` turns the limiter off.
- [x] The existing login and sign-up limits still apply with their own budgets, and `middleware.RateLimit`'s signature is unchanged.
- [x] `X-Forwarded-For` from an untrusted peer logs one warning per process.
- [x] `docs/configuration.md`, `docs/api-reference.md` (a Rate limits section) and `docs/deployment.md` describe the behaviour.

### 02: quotas

- [x] With every quota at `0`, behaviour is unchanged.
- [x] An owner at `quota.*.repos` can't create, fork, import, generate from a template, accept a transfer of, or restore a repository. Each of those is refused with a message stating the count.
- [x] `size_bytes` is backfilled at startup and recomputed after pushes, web commits, wiki writes, imports, forks, template creations and `cloudzilla gc`.
- [x] A push that would take the owner past `quota.*.storage_bytes` is refused over HTTP (413) and SSH, and a delete-only push still succeeds.
- [x] Web commits and wiki writes are refused when the owner is at or over the storage quota.
- [x] Soft-deleted repositories don't count toward either quota.
- [x] A superadmin's personal account is exempt. Orgs are not.
- [x] Settings and org settings show usage against each quota that is set.
- [x] Every `quota.*` key loads from YAML and from its `CZ_*` variable, and a negative value fails startup.
- [x] `docs/configuration.md`, `docs/api-reference.md` and the ROADMAP 19.3 entry describe the behaviour.

## Relevant files

- `internal/middleware/rate_limit.go`, `rate_limit_test.go`: the existing limiter, to generalise
- `internal/middleware/client_ip.go`: `ClientIP`, `RemoteIP`, `server.trusted_proxies`
- `internal/middleware/auth.go`: `extractToken`, `servePAT`, `serveOAuth`, `PATValidator`, `OAuthTokenResolver`
- `internal/handler/git_http.go`: `resolveGitUser`, the receive-pack size cap
- `internal/ssh/server.go`: the SSH receive-pack size cap
- `internal/gittransport/limit.go`: `LimitedReadCloser`
- `internal/handler/error_handler.go`: `isAPIRequest`, and the home for `onLimited`
- `internal/router/router.go`: the global middleware order and the existing per-route limits
- `internal/config/config.go`: the new `RateLimitConfig` and `QuotaConfig`
- `internal/service/repo_dirs.go` (`claimRepo`), `repo_service.go` (`Create`, `Fork`, `CreateFromTemplate`, `Restore`, `OnPostReceive`), `repo_import.go`, `import_service.go`, `repo_transfer.go`, `org_service.go`
- `internal/service/code_service_files.go` (`CommitFile`), `code_service_wiki.go`
- `internal/store/repo_store.go` (`CountForUser`)
- `cmd/cloudzilla/gc.go`, `internal/gitgc/gitgc.go`
- `internal/view/pages/settings.templ`, `org_settings.templ`
- `docs/configuration.md`, `docs/api-reference.md`, `docs/deployment.md`, `docs/ROADMAP.md`

## Out of scope

- Admin-panel configuration and a `rate_limit_config` table
- Per-owner quota overrides
- Postgres-backed rate counters, which come with Phase 20.2
- Git over SSH rate limits. SSH needs a key, so it is never anonymous, and it already has an idle timeout and `git.ssh_max_session`

## Decisions

Settled with the maintainer on 2026-10-06:

1. Quotas are in scope: both repository count and storage, each per owner.
2. Limits come from config and `CZ_*` variables only. There is no admin page and no `rate_limit_config` table.
3. Authenticated requests are bucketed per user, split into `web` (sessions) and `token` (PATs and OAuth tokens).
4. The rate limiter is on by default, and `ClientIP` warns about a misconfigured proxy.
5. The resources are `core`, `git`, `archive` and `search`, with the default numbers above. Blame, contributors and pulse stay in `core`.
6. The algorithm is a fixed window, extending the existing limiter. No new dependency.
7. `/auth/2fa/verify` gets no limit of its own.
8. Quotas are the same for every owner, off by default (`0`), and a superadmin's personal account is exempt.
9. Storage counts live repositories plus their wikis. Soft-deleted repositories don't count, and restoring one checks the quota.
10. Each push is capped at the space left, and delete-only pushes still go through.
11. Usage is shown as one line in user Settings and org settings.
12. Two PRs: rate limits first, then quotas.

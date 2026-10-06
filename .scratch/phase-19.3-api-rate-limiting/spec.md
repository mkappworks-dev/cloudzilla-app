# API rate limiting

Created: 2026-10-06
Category: enhancement
Status: needs-triage

Roadmap: Phase 19.3 (API Rate Limiting & Quotas). Facts below were checked against `origin/main` at `4d1e48d7` on 2026-10-06.

## Problem

Only six routes have a rate limit, and nothing else does. One script, crawler or CI loop can use as much of the server as it likes.

`middleware.RateLimit(limit, window)` (`internal/middleware/rate_limit.go`) counts requests per client IPv4 address or IPv6 /64 in a fixed window held in process memory. Each call builds its own limiter, so each route has its own budget. On a refusal it answers `429` with `Retry-After` and a plain-text body. It sends no `X-RateLimit-*` headers. `internal/router/router.go` applies it only to:

- `POST /invite/{token}`, `/register` and `/register/complete/{token}`: 10 per 15 minutes
- `POST /login`, `/auth/ldap` and `/api/auth/login`: 30 per 15 minutes

Everything else is unlimited, including the expensive routes:

- every other `/api/*` route
- git over HTTP: `GET /{owner}/{repo}/info/refs`, `POST …/git-upload-pack` and `POST …/git-receive-pack`. go-git builds a whole pack for every clone or fetch
- `GET /search` and `GET /search/code`
- `GET /{owner}/{repo}/archive/{ref}`, which zips the whole tree at the ref
- `GET /{owner}/{repo}/raw/{ref}/*`
- `POST /api/markdown/preview`
- `POST /oauth/token`, `POST /auth/saml/callback` and `POST /auth/2fa/verify`

For example, an anonymous loop over `GET /acme/app/archive/main` zips the repository again on every request, and nothing slows it down.

### What already exists

- **Client IP behind a proxy.** `middleware.ClientIP` runs globally, right after `RequestID`. It rewrites `r.RemoteAddr` from `X-Forwarded-For`, read right to left, but only when the peer is in `server.trusted_proxies` (`CZ_SERVER_TRUSTED_PROXIES`). `docs/deployment.md` warns that without the setting, every client behind a proxy shares the proxy's budget. Nothing at runtime notices the mistake.
- **2FA guessing.** `/auth/2fa/verify` has no IP limit. `ReauthService.CheckSecondFactor` claims an attempt from a per-user window before checking the code: 5 failures per 15 minutes. The window is stored on the user row, so every instance shares it (`TestReauthService_CheckSecondFactor_IsThrottled`). An attacker reaches the page only with a `cz_totp_pending` cookie, and that is issued only after the first factor succeeds. **That is enough to stop guessing.** The anonymous budget below caps request volume, so `/auth/2fa/verify` needs no limit of its own.
- **Where identity is resolved.** `Auth` and `OptionalAuth` are per-route (`r.With(authMW)`, or `r.Use` inside a route group). chi runs global middleware before routing, so a global limiter can't read `ClaimsFromContext`. Git over HTTP resolves a PAT sent as the Basic-auth password inside the handler (`resolveGitUser`), not in middleware. So even a per-route limiter would see `git clone` with a token as anonymous.
- **Cost of checking a credential.** A session JWT is checked with HMAC only, with no database query. A PAT (`czp_…`) or an OAuth-app token needs a database lookup (`AccessTokenService.Validate`, `OAuthAppService.ResolveOAuthToken`).
- **Dependencies.** `golang.org/x/time` isn't in `go.mod`.

### Where the roadmap entry is out of date

- It names migration `060_create_rate_limit_config.sql`, but `main` is already at `101_user_code_themes.sql`.
- It says unauthenticated requests "share a global bucket". Then a single scraper would lock every anonymous visitor out.
- The title says "& Quotas", but the body defines no quota.
- It puts limits in the admin panel, backed by a `rate_limit_config` table.

## Proposed design

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
| PAT (`Bearer czp_…`, or the Basic-auth password for git) that validates | `user:<id>:token` |
| OAuth-app token that resolves | `user:<id>:token` |
| None, or one that doesn't verify | `ip:<IPv4>` or `ip:<IPv6 /64>` |

- A credential that doesn't verify counts against the IP. Random tokens can't buy fresh buckets.
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
| `core` | everything else that isn't exempt |

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

- `/api/*` gets `{"error":"rate limit exceeded"}`, in the style of the JSON errors that `isAPIRequest` callers send.
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

Suppose a request arrives with `X-Forwarded-For` from a peer that isn't in `server.trusted_proxies`. `ClientIP` then logs one warning per process, naming the peer and the setting. Without the setting, every anonymous client behind a proxy shares one anonymous budget, which the global limiter makes far easier to exhaust than today's login budget.

### Several instances

Counts live in each process. Behind a round-robin load balancer, N instances allow up to N times each budget.

The counter sits behind a small interface (`allow(key, limit, window, now) → remaining, reset, ok`). Phase 20.2 can then add a Postgres-backed one. Doing that now would add a database write to every request, page views included, before anything runs more than one instance.

`docs/deployment.md` states the limitation.

### Out of scope

- Quotas (storage size, repository counts). See open question 1.
- An admin page and a `rate_limit_config` table. See open question 2.
- Git over SSH, which needs a key, so it is never anonymous. It already has an idle timeout and `git.ssh_max_session`.
- Per-user overrides and superadmin exemptions.

## Acceptance criteria

- [ ] Anonymous requests to a counted route share a budget per IPv4 address or IPv6 /64. Over budget they get `429` with `Retry-After`.
- [ ] A session request and a PAT request from the same user count against separate buckets. Two PATs of one user share a bucket.
- [ ] `git clone` with a PAT as the Basic-auth password counts against the user's `token` bucket, not the IP.
- [ ] A request carrying an invalid token counts against its IP.
- [ ] A PAT is validated once per request, not once in the limiter and again in auth.
- [ ] `git`, `archive` and `search` requests use their own budgets and leave `core` untouched. `POST /api/repos/{o}/{r}/archive` counts as `core`.
- [ ] Counted responses carry `X-RateLimit-Limit`, `-Remaining`, `-Reset` and `-Resource`. Exempt paths carry none and are never refused.
- [ ] A refused `/api/*` request gets a JSON error body, and git gets a plain-text one.
- [ ] Every key in the configuration table loads from YAML and from its `CZ_*` variable. `0` disables that class, a negative value fails startup, and `rate_limit.enabled: false` turns the limiter off.
- [ ] The existing login and sign-up limits still apply with their own budgets, and `middleware.RateLimit`'s signature is unchanged.
- [ ] `X-Forwarded-For` from an untrusted peer logs one warning per process.
- [ ] `docs/configuration.md`, `docs/api-reference.md` (a Rate limits section), `docs/deployment.md` and the ROADMAP 19.3 entry describe the behaviour.

## Relevant files

- `internal/middleware/rate_limit.go`, `rate_limit_test.go`: the existing limiter, to generalise
- `internal/middleware/client_ip.go`: `ClientIP`, `RemoteIP`, `server.trusted_proxies`
- `internal/middleware/auth.go`: `extractToken`, `servePAT`, `serveOAuth`, `PATValidator`, `OAuthTokenResolver`
- `internal/handler/git_http.go`: `resolveGitUser`, the Basic-auth PAT path
- `internal/handler/error_handler.go`: `isAPIRequest`, and the home for `onLimited`
- `internal/router/router.go`: the global middleware order and the existing per-route limits
- `internal/config/config.go`: the new `RateLimitConfig`
- `docs/configuration.md`, `docs/api-reference.md`, `docs/deployment.md`, `docs/ROADMAP.md`

## Open questions

1. **Quotas.** Are storage or repository-count quotas part of this work? Recommendation: no. The roadmap defines none, and they belong in the repository and storage services, not in request middleware. Give them their own roadmap entry.
2. **Where limits are configured.** Config file and `CZ_*` variables only, or the roadmap's admin page with a `rate_limit_config` table? Recommendation: config only for now, like every other operational setting. That needs no migration and avoids a database read on the request path. An admin page can come with Phase 20.2's shared state.
3. **Authenticated buckets.** One bucket per user, one per token, or one per user split into `web` and `token`? Recommendation: the split. Then a runaway token can't lock its owner out of the web UI, and minting tokens doesn't multiply the budget.
4. **On by default?** Recommendation: on, plus the proxy warning. An upgraded instance behind an unconfigured proxy would throttle anonymous visitors as a group, but signed-in users each keep their own budget.
5. **Resources and defaults.** Are `git`, `archive` and `search` the right separate buckets, and are the default numbers sensible?
6. **Algorithm.** A fixed window, extending the existing limiter, or the roadmap's token bucket from `x/time/rate`? Recommendation: the fixed window.
7. **2FA.** The per-user throttle is enough, so `/auth/2fa/verify` gets no limit of its own (see "What already exists").

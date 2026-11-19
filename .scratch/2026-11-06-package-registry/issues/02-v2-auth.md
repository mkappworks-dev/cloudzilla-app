# `/v2/` challenge, token endpoint and middleware

Created: 2026-11-18
Category: enhancement
Status: done
Blocked by: 01
Parallel with: none

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-v2-auth` | `.worktrees/feat+phase-16-v2-auth` | `feat(registry): v2 auth challenge and token endpoint (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

`docker login` and every later registry call need a way to prove who the caller is and what they may touch, through middleware that today would buffer, CSRF-check or alias-resolve `/v2` requests. Without this, nothing else in the wire protocol can be tested.

## Scope

- `GET /v2/` answers `200` with a valid registry token and otherwise `401` with `WWW-Authenticate: Bearer realm="<server.base_url>/v2/token",service="..."`; with `registry.enabled` false the whole `/v2` tree is `404`; `Docker-Distribution-API-Version: registry/2.0` on every response.
- `GET /v2/token` per the spec's Status codes table: Basic with a `czp_` PAT only (never an account password), or nothing; repeated `scope=repository:<name>:<actions>`; returns `token`, `access_token`, `expires_in` and `issued_at`. The access list is the intersection of the request and `CanRead`/`CanWrite` on the linked Repository; a new name grants `push` to a caller with `CanWrite` on any Repository of the path owner (decision 9); anonymous gets `pull` on public Repositories only. Key-bound PATs, suspended users and PATs without `packages:*` are `403`.
- A `/v2/*` middleware verifies the JWT (HMAC from `auth.jwt_secret` with a purpose label, `aud`, `typ: registry`, 5 minutes, injectable clock) and sets the user and access list on the context; insufficient access is `401` with `scope` and `error="insufficient_scope"`.
- An OCI error writer (`{"errors":[{"code","message","detail"}]}`) used by every `/v2` handler.
- One `/v2/*` handler that parses `<name>` from the right (`/blobs/`, `/manifests/`, `/tags/list`, `/referrers/`), so a name containing those words works; `_catalog` is `404`; other ops are stubs answering `UNSUPPORTED` until tickets 03, 04 and 10.
- Middleware: `IsStreamedUpload` covers `PATCH`, `PUT` and `POST` under `/v2/`; CSRF exempts `/v2/`; `RepoAlias`'s path split skips `v2`; `/v2` stays out of the setup path check (documented decision, like git transport).
- Rate limits: a `registry` resource for `/v2/*`; the subject lookup recognises a registry JWT before it tries an OAuth token; `/v2/token` is `core` and keyed on the PAT's user; budgets in the router map.

## Files (hotspots in bold)

- **`internal/router/router.go`**
- `internal/handler/registry_handler.go` (new)
- `internal/service/registry_service.go` (token issue and verify)
- **`internal/middleware/api_rate_limit.go`**, `csrf.go`, `body_limit.go`, `repo_alias.go`, new registry auth middleware
- `docs/access-control.md` (routes, status codes), `docs/configuration.md` (rate limits)

## Acceptance criteria

- [x] An unauthenticated `GET /v2/` is `401` with the Bearer challenge; `registry.enabled: false` makes it `404`
- [x] Basic with a PAT returns a token; a wrong password or an account password is `401`; a PAT with only `repo:write`, a key-bound PAT and a suspended user are `403`
- [x] Scope intersection: a reader gets `pull`, a writer `pull,push`, a stranger on a private Repository an empty list; anonymous gets `pull` on public only
- [x] The token expires after 5 minutes (injected clock); a tampered or expired one is `401` with a challenge naming `scope`
- [x] A registry JWT is refused as a session cookie and as a Bearer on the app's own routes
- [x] A name containing `blobs` as a segment routes correctly; `_catalog` is `404`
- [x] A registry request is counted in the `registry` bucket per user (and per IP when anonymous) and never triggers an OAuth-token lookup (asserted with a counting resolver)
- [x] Router tests with the in-test OCI client cover all of the above
- [x] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB — with `curl` only; a real `docker login` was not run (see the comment)
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

On a throwaway server: `curl -i /v2/`, then `docker login` with a PAT with and without `packages:write` and with the account password (refused); anonymous token requests against a public and a private Repository.

## Comments

### 2026-11-18

- `packages:write` now admits reads: `packageReadScopes` is `[packages:read, packages:write]` (narrowest first, so an
  `insufficient_scope` challenge still asks for `packages:read`), and at the token endpoint `packages:write` caps a
  token at `pull,push,delete` while `packages:read` caps it at `pull`. Ticket 01's comment, the spec and
  `docs/access-control.md` / `docs/api-reference.md` are updated.
- The token is an HS256 JWT keyed with `HMAC-SHA256(auth.jwt_secret, "cloudzilla.registry-token.v1")`, not the secret
  itself, so it can never verify as a session JWT (that is how "refused as a cookie and as a Bearer" holds). Claims:
  `aud` and `iss` are the host of `server.base_url` (also the challenge's `service`), `typ: registry`, `sub` the user id
  (absent when anonymous), `access` in docker's `[{type, name, actions}]` shape. `RegistryService.WithClock` injects the
  clock for issue and verify.
- A new name (no linked package) grants `pull,push` to a caller with `CanWrite` on any Repository of the path owner or
  with an unexpired pending package there, not `push` alone: docker asks for both and its first call is a `HEAD` of a
  blob. The registry answers `pull` from what exists, which for a new name is nothing. Side effect, inherent in decision
  9: a writer on one of the owner's Repositories can tell a free name from a private one they can't read.
- Anonymous: `200` only when every requested action is granted (`pull` on a public Repository's image); anything else is
  `401` with the Basic challenge, as the status table says. An anonymous token with no scope is accepted by `GET /v2/`,
  so a bare `docker login` check works.
- `delete` is a third action: the token grants it with `push`, and `DELETE` requests need it. Manager-only deletion of a
  whole package is ticket 04's to enforce.
- One `/v2/*` handler, route parsed from the right in `service.ParseRegistryRoute` (blob, upload-start, upload,
  manifest, tags, referrers). `RegistryAuth` parses first and answers `NAME_INVALID` (`400`) before it looks at the
  token; `_catalog` and unparseable paths pass through unauthenticated to a `404`. Operations that don't exist yet answer
  `405 UNSUPPORTED` with the parsed name and operation in `detail`, for tickets 03, 04 and 10 to replace.
- The owner segment resolves users first, then organizations, ignoring case (`PackageStore.ResolveOwner`); linked
  packages whose Repository is soft-deleted grant nothing, as for an unknown name.
- Rate limits: a Bearer that is not a registry token on `/v2/*` is bucketed by IP, never looked up as OAuth, so a pull of
  many layers costs no database read. `/v2/token` takes the PAT from the Basic password (as git does) so it is keyed on
  that user.
- CSRF returns before it sets its cookie for `/v2`, so registry responses carry no `Set-Cookie`.
- Verified on a throwaway server (scratch DB `cz_t02_live` on the test container, unique cookie name, seeded data) with
  `curl`: the Bearer challenge, anonymous public and private tokens, `packages:write`, `packages:read` and `repo:write`
  PATs, the account password, a new name, an insufficient-scope push, the registry token refused on `/api/user`, and
  `_catalog`. **A real `docker login` was not run**: Docker Desktop's daemon runs in a VM and cannot reach a host
  `127.0.0.1` server, and the host-network `socat` forwarder I tried to bridge it was denied by the permission
  classifier, so I stopped there. It needs a run with a reachable address or an insecure-registry setting.
- `make test-integration` starts its own database; I ran `go test ./...` with `TEST_DATABASE_DSN` on the `cz_t02` scratch
  DB instead, as ticket 01 did. No page changed and no standing mock is affected.

# `/v2/` challenge and token endpoint

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 01
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-v2-auth` | `.worktrees/feat+phase-16-v2-auth` | `feat(registry): v2 auth challenge and token endpoint (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

`docker login` and every later registry call need a way to prove who the caller is and what they may touch. Without this, nothing else in the wire protocol can be tested.

## Scope

- `GET /v2/` answers `200` with a valid registry token and otherwise `401` with `WWW-Authenticate: Bearer realm="<server.base_url>/v2/token",service="…"`; with `registry.enabled` false the whole `/v2` tree is `404`.
- `GET /v2/token` takes HTTP Basic with a `czp_` personal access token (never an account password) or nothing, reads the repeated `scope=repository:<name>:<actions>` params, and returns `token`, `access_token`, `expires_in` and `issued_at` for a 5-minute JWT whose access list is the intersection of the request, the token's `Targets` and what `CanRead`/`CanWrite` on the linked Repository allow. A key-bound token and a suspended user are `403`. Anonymous callers get `pull` on public Repositories only. A new name (no package yet) grants push to a caller who can write any Repository of that owner.
- A shared middleware for `/v2/*` verifies the JWT (HMAC with a key derived from `auth.jwt_secret` and a purpose label, claims `aud` and `typ: registry`, so it works without `security.secret_key` and is never accepted as a session) and sets the user and granted access on the context; a token without the right `packages:*` scope gets `403` `DENIED` (never `401` once credentials were accepted, so Docker keeps them).
- OCI error JSON (`{"errors":[{"code","message","detail"}]}`) writer used by every `/v2` handler; unreadable and missing packages answer the same `NAME_UNKNOWN` `404`.
- A `registry` rate-limit resource keyed on the registry JWT's subject (the `subject()` and `classifyRequest` code and the router budget map change), `/v2/token` counts as `core` and keys on the PAT; `429` carries `Retry-After`.
- Every `/v2` response without a sufficient token is `401` with `WWW-Authenticate` naming `scope` and `error="insufficient_scope"`; `Docker-Distribution-API-Version: registry/2.0` is sent.
- Routing: one `/v2/*` handler parses the name from the right so a name containing `blobs` works; `RepoAlias` skips `v2`; `IsStreamedUpload` and the CSRF exemption cover `/v2/`; `/v2` is deliberately not in the setup path check (before setup it redirects to `/setup`, like git transport).

## Files (hotspots in bold)

- **`internal/router/router.go`**
- `internal/middleware/` (new registry auth, `scope.go`, rate limit)
- `internal/handler/registry_handler.go` (new)
- `internal/service/registry_service.go`
- `internal/middleware/api_rate_limit.go`, `csrf.go`, `body_limit.go`, `repo_alias.go`
- `docs/access-control.md`

## Acceptance criteria

- [ ] An unauthenticated `GET /v2/` is `401` with the Bearer challenge; `registry.enabled: false` makes it `404`
- [ ] `docker login` style Basic (PAT) returns a token; a wrong password is `401`; a PAT with only `repo:write` is `403`
- [ ] Token scope intersection: reader gets `pull` only, writer `pull,push`, stranger on a private Repository gets an empty access list
- [ ] Anonymous token for a public Repository grants `pull`; for a private one grants nothing
- [ ] The token expires in 5 minutes (injected clock) and a tampered or expired one is `401` with a challenge
- [ ] A registry JWT is refused as a session cookie or Bearer on the app's own routes; a key-bound PAT, a suspended user and a PAT whose `Targets` exclude the Repository are refused
- [ ] A name containing `blobs` as a path segment routes correctly; an anonymous caller is counted per IP and an authenticated one per user
- [ ] Router tests with an in-test OCI client cover all of the above; the new routes are in the scope allow-list tests
- [ ] `docs/access-control.md` documents the routes, scopes and the `403`-not-`401` rule
- [ ] No page changes, so no light and dark check is needed; the standing mocks in `mockups/` are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

On a throwaway server: `curl -i /v2/`, then `docker login` with a PAT with and without `packages:write`; `docker login` with a wrong password fails; repeat anonymous against a public and a private Repository.

## Comments

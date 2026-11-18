# `/v2/` challenge, token endpoint and middleware

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
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

- [ ] An unauthenticated `GET /v2/` is `401` with the Bearer challenge; `registry.enabled: false` makes it `404`
- [ ] Basic with a PAT returns a token; a wrong password or an account password is `401`; a PAT with only `repo:write`, a key-bound PAT and a suspended user are `403`
- [ ] Scope intersection: a reader gets `pull`, a writer `pull,push`, a stranger on a private Repository an empty list; anonymous gets `pull` on public only
- [ ] The token expires after 5 minutes (injected clock); a tampered or expired one is `401` with a challenge naming `scope`
- [ ] A registry JWT is refused as a session cookie and as a Bearer on the app's own routes
- [ ] A name containing `blobs` as a segment routes correctly; `_catalog` is `404`
- [ ] A registry request is counted in the `registry` bucket per user (and per IP when anonymous) and never triggers an OAuth-token lookup (asserted with a counting resolver)
- [ ] Router tests with the in-test OCI client cover all of the above
- [ ] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

On a throwaway server: `curl -i /v2/`, then `docker login` with a PAT with and without `packages:write` and with the account password (refused); anonymous token requests against a public and a private Repository.

## Comments

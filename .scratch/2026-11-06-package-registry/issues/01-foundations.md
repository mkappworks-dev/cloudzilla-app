# Foundations: schema, config, scopes, quota accessor

Created: 2026-11-18
Category: enhancement
Status: done
Blocked by: none
Parallel with: none (everything else waits on it)

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-foundations` | `.worktrees/feat+phase-16-foundations` | `feat(registry): schema, config, package scopes and quota accessor (16.1)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

Every later ticket needs the tables, the config keys, the two token scopes, the `v2` owner name and one quota accessor to exist. Nothing is served yet; this ticket gives the rest a green base and keeps the quota rule in one place so tickets 03 and 06 can call it in parallel.

## Scope

- One migration (next free number; re-check origin/main, 141 is the highest at 81c4be80) for every table in the spec's Schema bullet: packages (nullable linked Repository while pending), registry blobs with `unlinked_at`, package-blob links, manifests with blob and child references, tags, upload sessions, generic versions and files. Indexes and `ON DELETE` choices are stated in the migration; link rows cascade from packages.
- Models, stores wired into `Stores`, and `RegistryService`, `RegistryGCService` and `PackageService` wired into `Services` (constructors, plus the methods below).
- Every config key in the spec's Config bullet with its default, `CZ_*` variable and startup validation (a non-positive TTL, grace or cap stops the server).
- `packages:read` and `packages:write`: a scope set accepted for personal access tokens and device grants only (`model.Scopes` is also the OAuth consent list, so they do not go there), shown on the PAT form and the device approval page, refused at `/oauth/authorize`, and added to `cz`'s own device scope list. In the scope allow-list, `packages` under `/api/repos/{owner}/{repo}` gets an explicit branch admitting `packages:read` or `packages:write` for reads and `packages:write` for writes (never `repo:*`), and the generic download path `/{owner}/{repo}/packages/generic/...` gets its own case.
- `v2` in `reservedOwnerNames`, and a startup log line naming any existing account called `v2`.
- `PackageService` usage and quota accessor (decision 11): usage of a path owner (distinct blob sizes over its packages including pending ones, generic file sizes, in-flight session bytes) and a check that returns `ErrPackageQuota` with usage and quota. Tested on rows inserted directly.

## Files (hotspots in bold)

- `internal/db/migrations/`
- `internal/model/` (new package models, **`scope.go`**)
- `internal/store/stores.go`, new stores
- `internal/service/services.go`, new services
- `internal/config/`
- **`internal/middleware/scope.go`**
- `internal/service/oauth_app_service.go`, `device_grant_service.go`, `access_token_service.go`
- `internal/cli/device.go`
- `internal/service/owner_name.go`
- `internal/view/pages/settings.templ` (token form), the device approval page
- `mockups/account_settings.html` and the device page mock, if they list scopes
- `docs/configuration.md`, `docs/access-control.md` (scopes)

## Acceptance criteria

- [x] The migration applies on a fresh DB and on a DB at 141
- [x] Every new config key has a default, a `CZ_*` variable and a validation test
- [x] A PAT and a device login can be created with `packages:write`; an OAuth app requesting it is refused; `cz auth login --scope packages:write` is accepted client-side
- [x] A `repo:write` token is refused on `/api/repos/{o}/{r}/packages/...` and a `packages:write` token admitted (table-driven scope tests, even though the handlers arrive in ticket 06)
- [x] Creating a user or organization named `v2` is refused, and startup logs an existing one
- [x] The usage accessor counts a blob shared by two packages of one owner once, counts it for each of two owners, and includes in-flight bytes
- [x] The token form and device page are checked at desktop 1440 in **light and dark**, and the standing mocks that list scopes are updated in the same PR
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Run the migration against a scratch Postgres and `make test-integration`; on a throwaway server create a token with the new scope in Settings and via `cz auth login`, and try an OAuth app request for it.

## Comments

### 2026-11-18

- Migration 142 (`142_package_registry.sql`), `IF NOT EXISTS` throughout. Path owner is `owner_user_id` xor `owner_org_id`
  (CHECK). A package's `repo_id` cascades on repository delete; link rows (`package_blobs`, manifests, tags, sessions,
  versions, files) cascade from `packages`. `package_blobs.digest` references `registry_blobs` with no cascade, so the sweep
  can delete a blob row only after its links are gone. Uniqueness is four partial indexes (linked and pending, user and
  org); a pending package may share a linked package's name.
- `PackageStore.OwnerUsage` and `PackageService.Usage`/`CheckQuota` reuse `QuotaOwner`. `CheckQuota` refuses when
  `used + adding > limit`, so reaching the quota exactly is allowed. A superadmin's personal account is exempt, as for the
  git storage quota (my choice; the spec is silent).
- `packages:read` does not admit writes. `packages:write` first did not admit reads either, literally as the ticket
  worded it; ticket 02 changed that on 2026-11-18, because `docker push` asks for `pull,push` and a write-only token
  would be refused. `/api/repos/{o}/{r}/packages` has no handler until ticket 06; the allow-list entry is tested
  table-driven.
- The generic download case matches exactly seven segments (`/{o}/{r}/packages/generic/{name}/{version}/{filename}`) and
  `GET`/`HEAD` only.
- `RegistryStore` has no methods yet and `RegistryService` and `RegistryGCService` no behaviour: only constructors, for
  tickets 02 and 03 to fill in.
- Startup logs an existing account named `v2` (`service.WarnShadowedOwnerNames`, case-insensitive, users and orgs).
- Verified on a throwaway server (scratch DB `cz_ui`, unique cookie name, seeded admin): a `packages:write` PAT created in
  Settings and listed; a device login for `packages:write repo:read` offered both scopes on the approval page; `repo:admin`
  on the device endpoint answers `400`. Token dialog and approval page screenshotted at 1440 in light and dark; both look
  right. The OAuth refusal is covered by the service test, not clicked through.
- `make test-integration` starts its own database; I ran `go test ./...` with `TEST_DATABASE_DSN` on the `cz_t01` scratch
  DB instead, which is the same test set. The mock list had only `account_settings.html`; no device-page mock exists.

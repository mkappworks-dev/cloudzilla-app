# Foundations: schema, config, scopes, reserved name

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: none
Parallel with: none (everything else waits on it)

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-foundations` | `.worktrees/feat+phase-16-foundations` | `feat(registry): schema, config and package scopes (16.1)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

Every later ticket needs the tables, the config keys, the two token scopes and the `v2` owner name to exist. Nothing is served yet; this ticket makes the rest start from a green base.

## Scope

- Migration (next free number; re-check origin/main, 141 is the highest at 81c4be80) for packages, registry blobs and per-repository links, manifests with blob and child references, tags, upload sessions, generic versions and files. The schema is described in the spec; each table has its indexes and `ON DELETE` choices stated in the migration.
- Models, stores wired into `Stores`, and empty `RegistryService` and `PackageService` wired into `Services` (constructor only).
- `registry.enabled`, `registry.max_blob_bytes`, `registry.upload_ttl`, `registry.gc_grace`, `packages.max_file_bytes`, `quota.{user,org}.package_bytes` and `rate_limit.registry.*` in the config, with `CZ_*` env vars, defaults and startup validation (a non-positive TTL or grace stops the server).
- `packages:read` and `packages:write` as known scopes in `model.Scopes` (which drives the PAT form, the device page and `cz auth login --scope`), PAT and device login only like `repo:admin`: OAuth apps cannot request them. `packages` joins the repo content resources in the scope allow-list, `packages:read` admitting `GET` and `packages:write` writes, with table-driven tests.
- `v2` and `packages` in `reservedOwnerNames`, and a startup log line listing any existing account that already has one of them (their profile pages would be shadowed).

## Files (hotspots in bold)

- `internal/db/migrations/`
- `internal/model/`
- `internal/store/stores.go`
- `internal/service/services.go`
- `internal/config/`
- `internal/middleware/scope.go`
- `internal/model/scope.go`, `cmd/cz`
- `internal/service/owner_name.go`
- `docs/configuration.md`

## Acceptance criteria

- [ ] Migration applies on a fresh DB and on a DB at 141
- [ ] Every new config key has a default, an env var and a validation test; `registry.enabled` defaults to true
- [ ] A token created with `packages:write` is accepted by the creation form and shown in Settings; an unknown `packages:*` scope is still `400`
- [ ] `cz auth login --scope packages:write` requests it and the approval page offers it (never widened past the request)
- [ ] Creating a user or organization named `v2` or `packages` is refused
- [ ] Existing scope tests still pass; a token without `packages:*` is refused on the packages API paths and an OAuth app cannot request the scopes
- [ ] No page changes, so no light and dark check is needed; the standing mocks in `mockups/` are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Run the migration against a scratch Postgres and `make test-integration`; create a token with the new scope in Settings on a throwaway server.

## Comments

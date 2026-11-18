# Garbage collection, purge and download counts

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 04, 06
Parallel with: 10

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-gc-purge-counts` | `.worktrees/feat+phase-16-gc-purge-counts` | `feat(registry): garbage collection, purge and download counts (16.1)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

Deleted images and aborted pushes must free disk without ever corrupting an image, deleting a Repository or an owner must take their packages with them, and the Packages pages need pull counts.

## Scope

- `RegistryGCService.Run` hourly in `main.go`, in batches of 500, exactly as the spec's GC bullet: (1) unreferenced links older than `registry.gc_grace`, expired pending packages and expired sessions with their temp files; (2) `unlinked_at` set on link-less blobs where it is null (cascades); (3) per blob, one transaction with `FOR UPDATE SKIP LOCKED`, a re-check that no link exists, the object delete, then the row delete. A hook between (3)'s read and its delete for tests.
- Purge (decision 15): `RepoService.PurgeExpired` deletes packages linked to the purged Repository; user and organization deletion delete packages under the path, including pending ones and generic files' rows. Objects are reclaimed by the sweep. Generic file objects (ticket 06) follow the release-asset pattern: rows first, objects after the commit, failures logged as orphans.
- Pull counts (decision 18): a manifest `GET` by tag or digest increments the manifest and the tag used in memory; flushed every minute and on shutdown; `HEAD` and blob reads do not count.

## Files (hotspots in bold)

- **`internal/service/registry_gc_service.go`** (new)
- `cmd/server/main.go`
- `internal/service/repo_service.go`, `user_service.go`, `org_service.go`, `services.go`
- `internal/handler/registry_handler.go` (counter calls)
- `docs/storage.md` (prefixes, the orphan rule), `docs/configuration.md`

## Acceptance criteria

- [ ] A deleted image's blobs go only after the grace (injected clock); an image sharing a layer keeps it
- [ ] Layers of an aborted push (linked, never referenced) are unlinked and then deleted after two graces
- [ ] A blob re-linked between the sweep's read and its delete survives with its object (the hook race test); a failed object delete leaves the row for the next run
- [ ] Purging a Repository removes only its linked packages; deleting the path owner removes everything under the path, including a package linked to a Repository that was transferred away
- [ ] Three pulls by tag and one by digest give the expected manifest and tag counts after a flush; shutdown flushes
- [ ] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server with `registry.gc_grace: 1m`: push, delete a tag and manifest, wait two sweeps (or trigger the sweep), confirm the object file is gone and a shared layer remains; abort a push mid-way and confirm its layers go.

## Comments

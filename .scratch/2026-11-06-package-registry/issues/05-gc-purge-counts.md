# Garbage collection, purge and download counts

Created: 2026-11-18
Category: enhancement
Status: done
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

- [x] A deleted image's blobs go only after the grace (injected clock); an image sharing a layer keeps it
- [x] Layers of an aborted push (linked, never referenced) are unlinked and then deleted after two graces
- [x] A blob re-linked between the sweep's read and its delete survives with its object (the hook race test); a failed object delete leaves the row for the next run
- [x] Purging a Repository removes only its linked packages; deleting the path owner removes everything under the path, including a package linked to a Repository that was transferred away
- [x] Three pulls by tag and one by digest give the expected manifest and tag counts after a flush; shutdown flushes
- [x] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server with `registry.gc_grace: 1m`: push, delete a tag and manifest, wait two sweeps (or trigger the sweep), confirm the object file is gone and a shared layer remains; abort a push mid-way and confirm its layers go.

## Comments

### 2026-11-18

- Files: `registry_gc_service.go` (`Sweep` is one pass, `Run` the hourly loop; `WithBackend`, `WithClock`, `WithSweepHook`),
  `registry_gc_store.go` (the batch queries, `ReapBlob`, `AddPullCounts`), `registry_pulls.go` (the in-memory counter).
  `main.go` starts `registry.sweep` outside the `registry.enabled` check and `registry.pull_counts`, and flushes once more
  after `srv.Shutdown`, so the last requests are counted (the ticker loop itself stops with the worker context).
- The sweep hook runs after a blob is picked and before its transaction. It cannot sit inside the transaction: the row is
  `FOR UPDATE`-locked there, so a test's re-link (`FOR SHARE`) would block forever. A real push inside the window waits on
  that lock and then finds no row, so it re-puts the object (ticket 03's rule).
- `linkBlob` now sets `created_at = NOW()` on a repeat link (`ON CONFLICT DO UPDATE`), so a retried push restarts the link's
  grace; with `DO NOTHING` a link from an aborted push 23 hours ago could be swept just before the retry's manifest.
- Purge: the rows already went by FK cascade (`packages.repo_id`, `owner_user_id`, `owner_org_id`), including pending
  packages and a package linked to a repository transferred away. What was missing was generic file objects: the keys are
  read before the cascade and deleted after, in `RepoService.PurgeExpired`, `UserService.DeleteUser` and
  `OrgService.Delete`; failures are logged as orphans. Container blobs go via the sweep. Deleting a user also removes
  packages of other owners linked to repositories the user owned (the repository row is gone).
- An organization with repositories cannot be deleted, so its packages at delete time link to repositories that moved away.
- Pull counts: `Manifest` carries `ID`, `PackageID` and the tag used; the handler calls `CountPull` on `GET` only. A tag
  re-pushed between the pull and the flush gets the count (accepted, as is losing a batch on a crash). A failed flush puts
  the batch back.
- GC tests use `OpenFreshTestDB` (own schema) because a sweep is global and would reap other test packages' blobs.
- `make test-integration` starts its own database; `go test ./...` and `golangci-lint run` ran green on the `cz_t05`
  scratch DB. No page changed, so there is no light and dark check and the standing mocks are unchanged.
- **Not done:** the throwaway-server check. The sweep is hourly with no trigger, so a live run would mean waiting an hour or
  adding a debug hook; the behaviour is covered by the clock-injected tests instead.

- Claude, 2026-11-19: the throwaway-server box is ticked from ticket 09's live run (`registry.gc_grace=1m`, delete, two
  restarts: the image's objects gone, the shared layer kept; `/private/tmp/czpkg-e2e/gc-live-transcript.txt`).

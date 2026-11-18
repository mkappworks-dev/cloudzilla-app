# Garbage collection, package quota and download counts

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 04, 06
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-gc-quota-counts` | `.worktrees/feat+phase-16-gc-quota-counts` | `feat(registry): garbage collection, quota and download counts (16.1)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

Deleted images must free disk, an owner must not be able to fill it, and the Packages pages need pull counts. These three need the blobs and manifests to exist and share the sweep and the counters.

## Scope

- An hourly sweep (registered in `main.go` like the attachment and log sweeps): blob objects unlinked for longer than `registry.gc_grace` (`unlinked_at`, set when the last link goes) are selected with one guarded statement that re-checks no link exists, linking takes a share lock on the blob row, and the object is deleted before the row so a failed delete is retried, in batches of 500; a sweep hook between its read and its delete lets a test race a push; expired upload sessions and their temp files go with it. A blob a concurrent push or mount just linked is never deleted.
- `RepoService.PurgeExpired` and user or organization deletion collect packages, manifests, links and generic files and delete the objects after the commit, as release assets do; an object delete that fails is logged and left as an orphan.
- `quota.{user,org}.package_bytes` checked at blob commit, manifest push and generic file upload (a quota hook added to the upload in ticket 06), `DENIED` naming usage and quota; usage counts each distinct blob once per owner plus generic files. `0` means unlimited. A usage accessor for ticket 08.
- Deleting an organization or a user purges packages under that path; purging a Repository purges its linked packages (a re-linked package follows its current Repository).
- Pull counts: a manifest `GET` counts on the manifest and on the tag used, batched in memory and flushed on a timer and on shutdown (a crash loses the unflushed batch: accepted and documented); `HEAD` and blob reads do not count.

## Files (hotspots in bold)

- **`internal/service/registry_gc_service.go`** (new)
- `cmd/server/main.go`
- `internal/service/repo_service.go`
- `internal/service/org_service.go`, `internal/service/services.go`
- `internal/service/user_service.go`
- `docs/storage.md`
- `docs/configuration.md`

## Acceptance criteria

- [ ] A deleted image's blobs are removed only after the grace period; an image sharing a layer keeps it
- [ ] A sweep racing a push or mount (blob re-linked between the sweep's read and its delete) loses nothing: tested with an injected clock and the sweep hook
- [ ] Purging a Repository, deleting a user and deleting an organization remove packages and objects; a failed object delete leaves a logged orphan and the row gone
- [ ] At the quota a push is `DENIED` with usage and quota in the message; with `0` nothing changes
- [ ] Counts: three pulls by tag and one by digest give the expected manifest and tag counts after a flush; a shutdown flushes
- [ ] `docs/storage.md` lists the new object prefixes and the "safe to delete" orphan rule
- [ ] No page changes, so no light and dark check is needed; the standing mocks in `mockups/` are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server with a short `registry.gc_grace`: push, delete a tag and manifest, wait, confirm the object file is gone and a shared layer remains; set a tiny `package_bytes` and push.

## Comments

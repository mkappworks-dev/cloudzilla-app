# Manifests, tags, linking and referrers

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 03
Parallel with: 06

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-manifests-and-tags` | `.worktrees/feat+phase-16-manifests-and-tags` | `feat(registry): manifests, tags and referrers (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

Docker needs `docker push` and `docker pull` to work end to end: a manifest ties the layers to a Repository, a tag names it, and `docker pull` by tag or digest must return the exact bytes. This ticket also decides which Repository an image belongs to.

## Scope

- `PUT /v2/{name}/manifests/{ref}` stores exact bytes (up to 4 MiB; OCI and Docker v2 types, image and index), rejects a manifest whose blobs or children are not linked to the package (`MANIFEST_BLOB_UNKNOWN`), writes the tag, and returns `201` with `Docker-Content-Digest`.
- The first manifest push links the pending package to a Repository: the image config's `org.opencontainers.image.source` label (as `owner/repo` or a URL on `server.base_url`) if it names a same-owner Repository the caller can `CanManage`, else a Repository named like the image that the caller can `CanWrite`, else a refusal naming both options. Later pushes require `CanWrite` on the linked Repository. A manager can re-link to another Repository they manage.
- `GET`/`HEAD` manifests by tag or digest with `Accept` negotiation and the stored `Content-Type`.
- `DELETE` by digest removes the manifest and its tags; `DELETE` by tag removes only the tag. Deleting a whole package is ticket 07.
- `GET /v2/{name}/tags/list` (`n`, `last`, `Link`) omitting referrer manifests; `GET /v2/{name}/referrers/{digest}` (with `artifactType` filter, `OCI-Subject` on push) from the database; `GET /v2/_catalog` is `404`.
- Platform summary of an index stored on the manifest for the UI.
- The queries ticket 07 renders (package list, tags with digests and sizes, untagged manifests, platforms) are `PackageService` methods built here, not in the UI ticket.
- A child manifest an index references cannot be deleted (`409`, `DENIED`); referrers sends `OCI-Filters-Applied: artifactType` when filtered and the index content type.

## Files (hotspots in bold)

- **`internal/handler/registry_handler.go`**
- **`internal/service/registry_service.go`**
- `internal/store/` (manifests, tags)
- `docs/package-registry.md` (started)

## Acceptance criteria

- [ ] Push then pull by tag and by digest returns identical bytes and digest; a re-push of a tag moves it and leaves the old manifest untagged
- [ ] A multi-platform index with two children pushes and pulls; deleting a child that an index references is refused
- [ ] Linking: label (both forms), name match and refusal each tested, plus a label naming another owner's Repository (refused), one the caller can write but not manage (refused), and a collaborator trying to claim another team's name (refused)
- [ ] Re-link by a manager works, including to a Repository of another owner after a transfer; a writer cannot re-link
- [ ] A reader cannot push or delete; a tag delete keeps the manifest; a manifest delete removes its tags
- [ ] Referrers: a manifest with `subject` appears under its subject and not in `tags/list`; an unknown subject returns an empty index, never `404`
- [ ] `_catalog` is `404`; `tags/list` of an unreadable package is `404`
- [ ] A rename of the linked Repository and a transfer leave the image path working
- [ ] No page changes, so no light and dark check is needed; the standing mocks in `mockups/` are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: real `docker push` and `docker pull` of a small image (and `docker buildx imagetools create` for an index) once this lands; `docker pull` by digest; confirm the refusal message is printed by the Docker client.

## Comments

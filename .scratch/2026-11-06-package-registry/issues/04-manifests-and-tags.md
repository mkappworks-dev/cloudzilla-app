# Manifests, tags, linking and delete

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 03
Parallel with: 06

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-manifests-and-tags` | `.worktrees/feat+phase-16-manifests-and-tags` | `feat(registry): manifests, tags and linking (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

`docker push` and `docker pull` must work end to end: a manifest ties the layers together, its first push decides which Repository the image belongs to, a tag names it, and a pull by tag or digest must return the exact bytes.

## Scope

- `PUT /v2/{name}/manifests/{ref}` stores exact bytes (up to 4 MiB; OCI and Docker v2, image and index), writes blob and child reference rows, rejects a manifest whose blobs or children are not linked to the package (`MANIFEST_BLOB_UNKNOWN`), writes the tag, and returns `201` with `Location` and `Docker-Content-Digest`.
- Linking at the first manifest push (decision 3): name match first (`CanWrite`), then the label (`owner/repo` or a URL on `server.base_url`, lenient on `.git`, trailing `/` and host case; another host ignored; `CanManage` on a Repository of the path owner), else a refusal naming both routes. The config blob is read through a 1 MiB cap. The pending package becomes linked; later pushes need `CanWrite` on the linked Repository.
- `GET`/`HEAD` manifests by tag or digest with `Accept` negotiation and the stored `Content-Type`.
- `DELETE` by digest removes the manifest and its tags (refused with `DENIED` while an index references it); `DELETE` by tag removes only the tag; blob `DELETE` is refused while a manifest of the package references the blob, otherwise removes the link row.
- `GET /v2/{name}/tags/list` listing every tag, lexically, with `n`, `last` and `Link`.

## Files (hotspots in bold)

- **`internal/router/router.go`** (shared with 03, 06, 10)
- **`internal/handler/registry_handler.go`**
- **`internal/service/registry_service.go`**
- `internal/service/package_service.go` (linking)
- `internal/store/` (manifests, tags, references)

## Acceptance criteria

- [ ] Push then pull by tag and by digest returns identical bytes and digest; a re-push of a tag moves it and leaves the old manifest untagged
- [ ] A multi-platform index with two children pushes and pulls; deleting a referenced child is refused
- [ ] Linking: name match, label as `owner/repo`, label as URL (with `.git` and trailing `/`), label on another host (ignored), and refusal; a label from a manager of `acme/tools` on image `acme/server` while `acme/server` exists links to `acme/server` and needs `CanWrite` there
- [ ] A label naming another owner's Repository, or one the caller can write but not manage, is refused; a config blob over 1 MiB is refused
- [ ] A reader cannot push or delete; a tag delete keeps the manifest; a manifest delete removes its tags; a referenced blob cannot be deleted
- [ ] `tags/list` paginates with `n` and `last` and a `Link` header; an unreadable package is the missing-name answer
- [ ] A rename of the linked Repository leaves the image path working
- [ ] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: real `docker push` and `docker pull` of a small image, `docker buildx imagetools create` for an index, `docker pull` by digest, and a push that is refused (check the Docker client prints the message).

## Comments

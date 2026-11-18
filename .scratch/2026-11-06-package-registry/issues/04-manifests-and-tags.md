# Manifests, tags, linking and delete

Created: 2026-11-18
Category: enhancement
Status: done
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

- [x] Push then pull by tag and by digest returns identical bytes and digest; a re-push of a tag moves it and leaves the old manifest untagged
- [x] A multi-platform index with two children pushes and pulls; deleting a referenced child is refused
- [x] Linking: name match, label as `owner/repo`, label as URL (with `.git` and trailing `/`), label on another host (ignored), and refusal; a label from a manager of `acme/tools` on image `acme/server` while `acme/server` exists links to `acme/server` and needs `CanWrite` there
- [x] A label naming another owner's Repository, or one the caller can write but not manage, is refused; a config blob over 1 MiB is refused
- [x] A reader cannot push or delete; a tag delete keeps the manifest; a manifest delete removes its tags; a referenced blob cannot be deleted
- [x] `tags/list` paginates with `n` and `last` and a `Link` header; an unreadable package is the missing-name answer
- [x] A rename of the linked Repository leaves the image path working
- [x] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: real `docker push` and `docker pull` of a small image, `docker buildx imagetools create` for an index, `docker pull` by digest, and a push that is refused (check the Docker client prints the message).

## Comments

### 2026-11-18

- New files: `registry_manifest_service.go`, `registry_link.go`, `registry_manifest_handler.go`, `registry_manifest_store.go`. The
  router needed no edit (ticket 02's `/v2/*` catch-all routes to `Registry`) and `package_service.go` is untouched:
  linking lives in `RegistryService`, which already owns pending-package resolution.
- Status codes: a referenced index child or blob is `409 DENIED`, quota `403 DENIED`, a refused link `403 DENIED` whose
  message names both routes (docker prints it as `error from registry: …`). A bad tag is `400 MANIFEST_INVALID`, a bad `n`
  is `400 PAGINATION_NUMBER_INVALID`, a body over 4 MiB is `413 SIZE_INVALID`.
- Name match is case-insensitive against the path owner's live Repositories and uses the image's first segment (`pub/server`
  links to `pub`). A label naming another owner, a missing Repository, another host or nonsense falls through to the same
  refusal. The 1 MiB config cap is checked against the stored blob size before it is read, and only when the label is
  needed, so a name-matched push never pays for it.
- `Accept` that excludes the stored media type answers `404 MANIFEST_UNKNOWN`. Tag delete and manifest delete need
  `CanWrite`; whole-package delete stays with ticket 10.
- Deleting a manifest or blob removes only rows (manifest, tags, reference rows, the package's link). The link row of a
  deleted manifest's blobs and the objects are left for the sweep (ticket 05). Re-pushing a tag to another manifest resets
  that tag's pull count.
- `subject` and `artifactType` are stored on push (the columns exist); `OCI-Subject`, the referrers `GET` and the platform
  summary are ticket 10, so `referrers` still answers `405 UNSUPPORTED`. Download counts (decision 18) are ticket 05.
- Verified on a throwaway server (scratch DB `cz_t04_live`, unique cookie name, server in an `alpine` container as the notes
  describe) with a real Docker client: `docker push` of an image named like a Repository (no label), pull by tag and by
  digest (the pulled bytes hash to the digest), a push linked by an `.git`-and-trailing-slash URL label (blob mounted from
  the other image), `docker buildx imagetools create` of a two-child index and a pull of it by tag and digest, and two
  refused pushes the client printed: an unlinked image (`error from registry: this image is not linked to a repository: …`)
  and a non-writer (`push access denied … insufficient_scope`). `curl` confirmed a referenced child is `409`, a tag delete
  keeps the manifest and `tags/list?n=1` sends `Link`. The seeded `admin@example.test` account has no repositories, so the
  PATs were inserted for two seeded users on the scratch DB.
- `make test-integration` starts its own database; `go test ./...` ran on the `cz_t04` scratch DB, as in tickets 01 to 03.
  No page changed, so there is no light and dark check and the standing mocks are unchanged.

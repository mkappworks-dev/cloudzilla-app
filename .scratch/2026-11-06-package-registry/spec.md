# Package registry (ROADMAP 16.1–16.2)

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent

Decisions confirmed by the maintainer on 2026-11-18 over three grill rounds and two independent reviews
(recommendations accepted as written). The design mock decisions are in [README.md](./README.md); the standing mocks
are `mockups/repo_packages.html`, `package_detail.html` and `owner_packages.html`. Umbrella ticket:
`../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`.

## Problem Statement

A team that builds a container image or a release tarball from a Cloudzilla Repository has nowhere on the Instance to
publish it. They push to Docker Hub or ghcr.io, so the artifact lives outside the permissions, quotas and backups of the
code it came from, and a private Repository's image has to be copied to a third party. Daisy owns `mkappworks/cloudzilla`
and wants `docker push git.example.com/mkappworks/cloudzilla-server:v0.4.1` to work, to see that image on the
Repository, and to have only people who can read the Repository pull it.

## Solution

The Instance becomes a **Registry**. Docker and podman log in with a personal access token and push and pull images
through the OCI Distribution v2 API at `/v2/`. A writer can also upload generic files with a plain HTTP `PUT`. Every
**Package** lives at `{owner}/{name}` and is linked to one Repository, from which it takes its permissions: readers
pull, writers push and delete, and there is no per-package visibility. The Repository gets a Packages tab (list, detail
with tags, digests, files and a pull command), the owner's profile gets a Packages tab across their Repositories, and
storage counts against an optional package quota. Blobs live on the existing storage `Backend`, so local and S3 both
work.

## Decisions

| #   | Decision |
| --- | -------- |
| 1   | Scope is OCI containers plus generic files. npm, PyPI, Go proxy, Maven and the rest are future work; the package model carries a `type` so they can slot in. |
| 2   | Image path is `{host}/{owner}/{name}`, no repo segment, lowercase, `/` segments allowed. The **path owner** is `{owner}`. |
| 3   | Linking happens at the first manifest push. If the path owner has a Repository named like the image's first segment, the image links there and needs `CanWrite` on it, whatever the label says. Otherwise the image config's `org.opencontainers.image.source` label decides: `owner/repo` or a URL on `server.base_url` (lenient: `.git` suffix, trailing `/`, host case), naming a Repository of the path owner on which the caller has `CanManage`. A label on another host is ignored. Otherwise the push is refused with a message naming both routes. The config blob read is capped at 1 MiB. |
| 4   | Docker and podman are the clients; `cz` has no OCI code. `docker login` sends a `czp_` personal access token, never an account password (which would bypass TOTP, suspension and SSO), to `GET /v2/token`, which returns a 5-minute JWT. |
| 5   | New scopes `packages:read` (pull, download) and `packages:write` (push, upload, delete) for personal access tokens and device login; OAuth apps cannot be granted them. `repo:*` scopes never admit package routes. |
| 6   | The registry JWT is an HMAC token keyed from `auth.jwt_secret` with a purpose label (so it works without `security.secret_key`), carries `aud` and `typ: registry`, is never accepted as a session, and may outlive a revoked PAT by its 5 minutes (accepted). |
| 7   | CI gets no registry token in v1: the `czj_` job token stays git-fetch only. A workflow uses a `packages:read`/`packages:write` PAT in `secrets.*` with `crane`, `skopeo` or `curl`. The Job container has no Docker daemon, so `docker push` from a Job is out. |
| 8   | Blob objects are shared and content-addressed (one per digest). **Per-package** link rows grant access, never the existence of the object. |
| 9   | Docker uploads blobs before the manifest, so the first upload to a new name creates a **pending package**: no Repository, visible to nobody, unique per (path owner, name, creator), usable only by its creator, at most 20 per user, and expiring 24 h after creation whatever happens. A new name needs `CanWrite` on at least one Repository of the path owner. |
| 10  | Uploads spool to a temp file under a named directory and are put with their exact size. Limits: 5 GiB per blob, 10 open sessions and 10 GiB in flight per user, sessions expire 24 h after creation. A restart loses in-flight uploads (accepted). |
| 11  | Quota keys `quota.user.package_bytes` and `quota.org.package_bytes`, default `0` (off), separate from the git storage quota. The **path owner** pays: usage is each distinct blob linked from the path owner's packages once, plus generic files, plus in-flight bytes. A pending package's blobs count against its path owner. Over the quota is `DENIED`. |
| 12  | Deleting a tag keeps its manifest as an untagged manifest. Deleting a manifest deletes its tags. A blob `DELETE` is refused while a manifest of the package references it. An hourly sweep unlinks unreferenced links (aborted pushes) older than 24 h and deletes unlinked blobs 24 h after their last link went. No auto-deletion of untagged manifests; no retention policies. |
| 13  | Acceptance is a real `docker login`, push and pull on a throwaway server, plus the OCI conformance suite run locally, results in the final PR body. |
| 14  | A Repository rename changes nothing (the path has no repo name). A transfer leaves the package at its path and linked to the transferred Repository, so pulls keep working; the package page notes the mismatch. The path owner (the user, or an owner of the organization) may always delete a package under its path or re-link it to one of its own Repositories. Re-linking never crosses to another owner. An owner rename breaks pulls under the old name (documented; owner aliases are future work). |
| 15  | Deleting the path owner purges everything under the path. Purging a Repository purges only the packages linked to it. |
| 16  | `CanRead` on the linked Repository is checked on every request, never cached publicly. A private, soft-deleted, unknown or pending name answers the same way as a missing one. |
| 17  | A separate `registry` rate-limit resource (`anonymous: 2000`, `authenticated: 20000` per window) keyed on the registry JWT's subject. `/v2/token` counts as `core`; a Basic PAT on the generic paths is keyed per user, not per IP. |
| 18  | A manifest `GET` (not `HEAD`, not blobs), by tag or digest, counts one download on the manifest and on the tag used. Counts are batched in memory and flushed every minute and at shutdown; a crash loses the batch (accepted). |
| 19  | The package page's readme card renders the linked Repository's default-branch README, with relative links resolved against the code tab. |
| 20  | Tags and versions are deletable with `CanWrite` on the linked Repository. Deleting a whole package needs `CanManage` there (or path ownership, decision 14) and a typed name. Re-linking is a path-owner or manager action within the path owner's Repositories. |
| 21  | Generic files: `PUT /api/repos/{owner}/{repo}/packages/generic/{name}/{version}/{filename}` and `GET /{owner}/{repo}/packages/generic/{name}/{version}/{filename}`, Bearer or Basic (PAT). Names outside `[A-Za-z0-9._-]`, `.` and `..` are `400`, never cleaned. A name already used by another Repository of the owner is `409`. sha256 per file, 512 MiB per file, re-upload is `409`. |
| 22  | Manifests with a `subject` are accepted and `GET /v2/{name}/referrers/{digest}` is served from the database. `tags/list` lists every tag; only untagged referrer manifests are left out of the UI's untagged list. |
| 23  | `GET /v2/_catalog` is `404`. `registry.enabled` (default true; false makes `/v2` answer `404`). The token realm is built from `server.base_url`. A reverse proxy must pass `/v2/` with no body limit and no buffering. `/v2` is not in the setup path check: before setup it redirects to `/setup`, as git transport does. |
| 24  | One PR for the spec, from the integration branch `feat/phase-16-package-registry`; each ticket's PR targets that branch. |

## Status codes

| Where        | Case                                                                           | Answer                                                                |
| ------------ | ------------------------------------------------------------------------------ | --------------------------------------------------------------------- |
| `/v2/token`  | Bad or missing credentials for a non-public scope                              | `401` with `WWW-Authenticate: Basic realm="registry"`                 |
| `/v2/token`  | A valid PAT without `packages:*`, a key-bound PAT, a suspended user            | `403` naming the reason                                               |
| `/v2/token`  | Valid credentials, no access to the requested name                             | `200` with an empty access list                                       |
| `/v2/*`      | No token, an expired token, or an access list without the needed action        | `401` with `WWW-Authenticate: Bearer realm,service,scope,error`       |
| `/v2/*`      | The token grants the action but the name is unknown, pending (not yours) or unreadable | `404` `NAME_UNKNOWN`                                          |
| `/v2/*`      | Quota, a blob still referenced, an index child                                 | `403` or `409` `DENIED` with the reason                               |
| Generic      | Basic PAT without the scope                                                    | `403`; unreadable or missing is `404`                                 |

A token with an empty access list therefore sees `401` for every name, public or private, existing or not, so private
names do not leak. Docker shows the same "unauthorized" for a reader pushing.

## User Stories

1. As a developer, I want to `docker login` to my Instance with a personal access token, so that I do not need a second
   credential.
2. As a developer, I want `docker push host/owner/name:tag` to store my image, so that the image lives next to its code.
3. As a developer, I want an image named like a Repository to link to it with no label, so that the simplest setup works.
4. As a developer, I want the `org.opencontainers.image.source` label to link an image whose name matches no
   Repository, so that I can choose the Repository.
5. As a developer, I want a refused push to say how to link the image, so that I do not guess.
6. As a developer, I want to push large layers in chunks, so that a 2 GiB layer does not need one request.
7. As a developer, I want to push a multi-platform image, so that one tag works on amd64 and arm64.
8. As a developer, I want to push the same layer to two images without uploading it twice, so that pushes are fast.
9. As a developer, I want `docker pull host/owner/name:tag` and `docker pull host/owner/name@sha256:…` to work, so that I
   can pin by digest.
10. As an anonymous visitor, I want to pull an image of a public Repository without logging in, so that open-source
    images are usable.
11. As a Repository reader, I want to pull a private Repository's image with a `packages:read` token, so that my
    deployment can use it.
12. As a Repository reader, I want not to be able to push, so that reading never changes what I deploy.
13. As a user whose token has only `repo:write`, I want a clear `403` naming the missing `packages:*` scope, so that I
    know what to add.
14. As a user without access to a private Repository, I want its images to look exactly like missing ones, so that
    private names do not leak.
15. As a team, I want our image name to stay ours even if another Repository's manager pushes first, so that a name
    matching our Repository cannot be claimed by a label.
16. As a Repository writer, I want to delete a tag, so that I can retire a version.
17. As a Repository writer, I want an untagged manifest listed and deletable, so that I can see what a re-push left
    behind.
18. As a Repository manager, I want to delete a whole package after typing its name, so that a writer cannot wipe it by
    accident.
19. As a path owner, I want to re-link a package to another of my Repositories, so that a mistaken link can be fixed.
20. As a Repository owner, I want the package to follow the Repository's visibility at once, so that making it private
    stops anonymous pulls.
21. As a Repository owner, I want a soft-deleted Repository's packages hidden and restored with it, so that Restore
    loses nothing.
22. As an owner who transfers a Repository, I want existing `docker pull` commands to keep working, so that consumers do
    not break.
23. As the path owner after a transfer, I want to keep the right to delete or re-link packages under my name, so that I
    am never locked out of my own namespace.
24. As a reader of a package whose Repository moved to another owner, I want a note on the page saying so, so that I
    understand its path.
25. As a release engineer, I want to upload a generic file with `curl -u user:token -T file`, so that CI can publish a
    tarball.
26. As a release engineer, I want several files under one version with a sha256 each, so that I can verify downloads.
27. As a release engineer, I want a re-upload of the same file refused, so that a published version never changes
    silently.
28. As a consumer, I want to download a generic file by URL, so that `curl -LO` is the install.
29. As a user of cosign or another signing tool, I want signatures stored next to an image through the referrers API, so
    that tools work.
30. As a Repository reader, I want the Packages tab to list packages with type, latest version, downloads and updated
    time, so that I can find the right one.
31. As a Repository reader, I want to filter the list by type and name, so that a long list is manageable.
32. As a Repository reader, I want a package page with a copyable `docker pull` command, so that I need no manual
    typing.
33. As a Repository reader, I want a tags table with digest, size and publisher, so that I see exactly what a tag points
    at.
34. As a Repository reader, I want the supported platforms of a multi-platform tag, so that I know it fits my machine.
35. As a Repository reader, I want the Repository's README on the package page, so that usage notes are one click away.
36. As a profile visitor, I want an owner's Packages tab across their Repositories, so that I can browse an
    organization's images.
37. As an owner, I want my package storage and its quota shown in settings, so that I see what I use.
38. As an operator, I want `quota.*.package_bytes` off by default, so that upgrading changes nothing.
39. As an owner at the quota, I want a push refused with a message naming usage and quota, so that I know what to
    delete.
40. As an operator, I want unreferenced blobs swept automatically after a grace period, so that deleted images and
    aborted pushes free disk.
41. As an operator, I want an in-flight push never to lose its layers to the sweep, so that GC cannot corrupt an image.
42. As an operator, I want the registry on S3 as well as local disk, so that it follows my storage choice.
43. As an operator, I want `registry.enabled: false` to remove the whole `/v2` surface, so that I can run without it.
44. As an operator, I want a separate rate-limit bucket for the registry, so that a pull of a large image does not
    exhaust the web budget.
45. As an operator, I want per-user caps on open sessions, in-flight bytes and pending packages, so that one user cannot
    fill the temp disk or squat names.
46. As an operator, I want docs for the reverse proxy settings `/v2/` needs, so that pushes do not fail at the proxy.
47. As a Cloudzilla user, I want `cz auth login --scope packages:write` to mint a registry token, so that I can paste it
    into `docker login`.
48. As a security reviewer, I want blob access decided by the package's link rows, so that knowing a digest never reads
    another package's layer.
49. As a security reviewer, I want mount to need read access to the source package, so that mount cannot copy a private
    layer.

## Implementation Decisions

- **Modules.** A `RegistryService` (token issue and verify, upload sessions, blobs, manifests, tags, referrers, delete),
  a `RegistryGCService` (the sweep), a `PackageService` (generic packages, package list and detail, deletion, linking,
  re-linking, quota and usage), stores for each, and handlers `registry_handler.go` (the `/v2/` wire protocol, which
  writes OCI error JSON, not the app's `writeJSON` errors), `package_handler.go` (generic API, JSON, and the form
  `POST`s for delete and re-link) and `package_page_handler.go` (`Page*` only). Handlers call services only.
- **Schema (migration 142 onward; re-check origin/main).** `packages`: path owner, type, name, linked Repository
  (nullable while pending), creator, pending expiry. Registry blobs: digest, size, `unlinked_at`. Package-blob links:
  package, digest, created. Manifests: package, digest, media type, size, exact bytes, subject digest, artifact type,
  platform summary, pull count, with manifest-blob and manifest-child reference rows so GC and the platform display need
  no JSON parsing. Tags: package, name, manifest, pusher, pull count. Upload sessions: id, package, user, bytes so far,
  created, temp path. Generic versions and files: name, size, sha256, storage key, uploader. Each ticket that needs a
  table takes the next free migration number.
- **Linking.** Decision 3 at the first manifest push; later pushes need `CanWrite` on the linked Repository. Re-link
  (decision 14) is a `PackageService` method used by the page; it never crosses owners.
- **Names.** The OCI name regex, at most 255 characters with host. The owner segment resolves case-insensitively to a
  user or organization; an owner whose name can never be a valid path (uppercase-only matches resolve, trailing
  separators do not) gets a clear refusal at push. A name may contain segments called `blobs`, `manifests`, `tags` or
  `referrers`, so one `/v2/*` handler parses the route from the right. `v2` joins `reservedOwnerNames`, and startup logs
  any existing account named `v2`.
- **Auth.** See Status codes. The token endpoint reads repeated `scope=repository:<name>:<actions>` params and returns
  `token`, `access_token`, `expires_in` and `issued_at`; the access list is the intersection of the request and what
  `CanRead`/`CanWrite` on the linked Repository (or decision 9 for a new name) allow. Anonymous requests get `pull` on
  public Repositories only. `/v2/` and every `/v2` response send `Docker-Distribution-API-Version: registry/2.0`.
- **Scopes.** `model.Scopes` is also the OAuth consent list, so the packages scopes go in a separate set accepted for
  personal access tokens and device grants only (beside the existing `IsTokenScope` handling of `repo:admin`), and
  `cz`'s own device scope list grows to match. In the scope allow-list, `packages` under `/api/repos/{owner}/{repo}`
  gets an explicit branch admitting `packages:read` or `packages:write` (reads) and `packages:write` (writes), and the generic download
  path gets its own case. `/v2` has its own JWT check and never passes through the allow-list.
- **Uploads.** `POST` starts a session bound to the user and package; `PATCH` appends to the temp file, with
  `Content-Range` optional (Docker usually omits it) and checked when sent; the final `PUT` (or a monolithic
  `PUT`/`POST ?digest=`) verifies the `sha256` digest and size, puts the object with its exact size if it is not already
  stored, then writes the link row. Session and pending-package expiry are checked lazily on every access; the sweep
  only reclaims. The temp directory is emptied at startup. The handlers lift the server's read and write deadlines.
  Cross-package mount copies only a link row, after `CanRead` on the source package; otherwise `202` and a normal
  upload. Blob `Range` is served by skipping bytes in the stream, because `Backend` has no ranged read (cost accepted).
- **Manifests.** Exact bytes, up to 4 MiB, OCI and Docker v2 image and index types. A manifest whose blobs or children
  are not linked to the package is `MANIFEST_BLOB_UNKNOWN`. `PUT …?tag=` multi-tag is not implemented. Tag list is
  paginated by `n`, `last` and `Link`. Referrers support `artifactType` filtering with `OCI-Filters-Applied` and send
  `OCI-Subject` on push. A child an index references cannot be deleted.
- **GC.** `RegistryGCService.Run`, hourly, registered in `main.go` like the attachment and log sweeps. Per batch of 500:
  (1) delete link rows no manifest of the package references and older than `registry.gc_grace`, and expired pending
  packages and upload sessions with their temp files; (2) set `unlinked_at` on blobs with no link and a null
  `unlinked_at` (covers links removed by cascades); (3) in one transaction, `SELECT … FOR UPDATE SKIP LOCKED` blobs
  unlinked longer than the grace, re-check that no link exists, delete the object, then the row. Linking takes
  `FOR SHARE` on the blob row and re-puts the object if the row is gone, so a push waits for or survives a concurrent
  delete. A failed object delete aborts that blob's transaction and is retried next hour. A hook between the read and
  the delete lets a test race a push.
- **Purge.** `RepoService.PurgeExpired` deletes packages linked to the purged Repository; user and organization
  deletion delete packages under the path. Objects go via the sweep (their links are gone), never inline.
- **Quota.** Decision 11, a `PackageService` usage accessor checked at blob commit, at each `PATCH` (with in-flight
  bytes), at manifest push and at generic upload.
- **Rate limits.** The `registry` resource is classified for `/v2/*`. The subject lookup recognises a registry JWT
  before it tries an OAuth token (which costs a database read), and keys a Basic PAT on the generic paths per user.
- **Middleware.** `IsStreamedUpload` (today `POST` on the release-asset path only) extends to `PATCH`, `PUT` and `POST`
  on `/v2/` and the generic upload path; CSRF exempts `/v2/` and Basic-authenticated generic uploads; `RepoAlias`'s path
  split skips `v2`.
- **Generic.** Upload streams to a temp file while hashing, then puts the object; its key ends in a random token, never
  the user's name. Download responses carry `Content-Disposition: attachment`, `nosniff`,
  `Content-Security-Policy: default-src 'none'; sandbox`, `Cache-Control: private, no-cache`.
- **README.** A `CodeService` method returns the rendered default-branch README of a Repository with relative links and
  images resolved against the code tab, extracted from what the repo page already does.
- **UI.** `Packages` is a repository tab after Releases and a tab on the owner profile. Pages follow the three standing
  mocks and use the existing table, tabs, badge, dialog and copy-button components.
- **Config.** `registry.enabled` (true), `registry.max_blob_bytes` (5 GiB), `registry.max_sessions_per_user` (10),
  `registry.max_inflight_bytes_per_user` (10 GiB), `registry.max_pending_per_user` (20), `registry.upload_ttl` (24h),
  `registry.gc_grace` (24h), `packages.max_file_bytes` (512 MiB), `quota.{user,org}.package_bytes` (0),
  `rate_limit.registry.anonymous` (2000), `rate_limit.registry.authenticated` (20000), each with a `CZ_*` variable.
- **Docs.** A new `docs/package-registry.md`, a line in CLAUDE.md's subsystem list, updates to `access-control.md`
  (scopes, routes, status codes), `storage.md` (layout, cleanup, orphans), `configuration.md`, `api-reference.md`,
  `cli.md` (scopes), `ci.md` (the secrets recipe), `organizations.md` (an org rename breaks pulls) and ROADMAP status.
  Each ticket writes the docs for what it builds; ticket 09 owns the new page's final shape and the cross-links.

## Testing Decisions

- **Seam:** the existing highest seam, router tests over a real database and the local `Backend` in a temp dir
  (`newVerificationRouter`, `internal/router/fork_test.go`). A small in-test OCI client drives the wire protocol: token,
  chunked push, manifest push, pull, tag list, referrers, delete. S3 runs behind the existing `TEST_S3_*` variables.
- A good test asserts external behaviour: status codes, OCI error codes, headers (`Location`, `Docker-Content-Digest`,
  `WWW-Authenticate`), the bytes pulled, and who sees what. It does not assert on key names or table layout.
- **Service tests** for the sweep (an injected clock and the hook between its read and its delete), quota arithmetic
  and the link rules. Token, session and pending expiry use the same injected clock. Memory bounds: a test streams a
  100 MiB body through a reader that fails if any single buffer the handler holds exceeds 1 MiB.
- **Cases:** anonymous pull of public and private, a reader pushing, a token with the wrong scope, a Repository made
  private mid-session, a soft-deleted Repository, a rename and a transfer, the path owner after a transfer, a label
  trying to claim a name-matched image, mount of an unreadable source, a digest mismatch, a chunk out of order, an
  expired session, a pending package someone else holds, a duplicate generic upload, quota reached, a sweep racing a
  push.
- **Gate:** a real `docker login`, push and pull on a throwaway server (scratch Postgres, unique
  `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), and the OCI conformance suite with the API toggles
  for pull, push, tags, referrers and delete, results in the final PR body. `podman` is documented, not tested.
- **Prior art:** `internal/router/fork_test.go`, `release_asset_service_test.go`, the workflow log sweep tests, the
  storage tests, the throwaway-server notes for live checks (light and dark at 1440 for the UI).

## Out of Scope

- npm, PyPI, Go module proxy, Maven, NuGet, Helm, and every other ecosystem.
- A registry or generic-package path for the `czj_` job token, Docker-in-Job, and any `cz` registry command.
- `GET /v2/_catalog`, `PUT ?tag=` multi-tag, cleanup and retention policies, per-package visibility, and a
  metadata-database style online GC.
- Signature display in the UI, vulnerability scanning, and mirroring or proxying another registry.
- Release asset enhancements (ROADMAP 16.3).
- Podman automated tests.
- Owner aliases, so an owner rename breaks existing pull commands.
- OAuth-app access to `packages:*`, and token targets for packages tokens (targets exist only on `repo:admin` tokens,
  which must be key-bound, which the registry refuses).

## Further Notes

- The registry runs in the same process and port as the app. A proxy that buffers request bodies or limits their size
  breaks layer pushes.
- Docker accepts plain HTTP only for `localhost`; elsewhere the Instance needs HTTPS.
- The registry assumes one instance: temp files are local and pull counts are in memory.
- The mock's open questions are answered by decisions 3, 5, 11, 12, 18 and 19.

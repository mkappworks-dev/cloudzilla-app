# Package registry (ROADMAP 16.1–16.2)

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent

Decisions confirmed by the maintainer on 2026-11-18 (recommendations accepted as written). The design mock decisions are
in [README.md](./README.md); the standing mocks are `mockups/repo_packages.html`, `package_detail.html` and
`owner_packages.html`. Umbrella ticket: `../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`.

## Problem Statement

A team that builds a container image or a release tarball from a Cloudzilla Repository has nowhere on the Instance to
publish it. They push to Docker Hub or ghcr.io, so the artifact lives outside the permissions, quotas and backups of the
code it came from, and a private Repository's image has to be copied to a third party. Daisy owns `mkappworks/cloudzilla`
and wants `docker push git.example.com/mkappworks/cloudzilla-server:v0.4.1` to work, to see that image on the Repository,
and to have only people who can read the Repository pull it.

## Solution

The Instance becomes a **Registry**. Docker and podman log in with a personal access token and push and pull images
through the OCI Distribution v2 API at `/v2/`. A writer can also upload generic files with a plain HTTP `PUT`. Every
**Package** is linked to one Repository and takes its permissions from it: readers pull, writers push and delete, and
there is no per-package visibility. The Repository gets a Packages tab (list, detail with tags, digests, files and a
pull command), the owner's profile gets a Packages tab across their Repositories, and storage counts against an
optional package quota. Blobs live on the existing storage `Backend`, so local and S3 both work.

## Decisions

| #   | Decision                                                                                                                                                                                                                                                                                                                                       |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Scope is OCI containers plus generic files. npm, PyPI, Go proxy, Maven and the rest are future work; the package model carries a `type` so they can slot in.                                                                                                                                                                                  |
| 2   | Image path is `{host}/{owner}/{name}`, no repo segment, name lowercase and may contain `/` segments. The first manifest push links it to a Repository: the image config's `org.opencontainers.image.source` label if it names a Repository of the same owner the caller can write, else a Repository named like the image, else a clear refusal. The label is accepted as `owner/repo` or a URL on `server.base_url`. Linking through the label needs `CanManage` on the target Repository; linking by name match needs `CanWrite`, so a minor-repo writer cannot claim a team's image name. |
| 3   | `docker login` sends a personal access token (never an account password, which would bypass TOTP, suspension and SSO) as the password to `GET /v2/token`, which returns a 5-minute JWT signed with a key derived from `auth.jwt_secret`. New scopes `packages:read` and `packages:write`, also requestable by `cz auth login`. Docker and podman stay the clients; `cz` has no OCI code.                                                                                  |
| 4   | CI does not get a registry token. A workflow uses a `packages:write` token in `secrets.*` with `crane`, `skopeo` or `curl`; the `czj_` token stays read-only. The Job container has no Docker daemon, so `docker push` from a Job is out.                                                                                                       |
| 5   | Blob objects are shared and content-addressed. Per-repository link rows grant access, never the existence of the object. Uploads spool to a temp file and are put with their exact size; sessions live 24 h; 5 GiB per blob.                                                                                                                  |
| 6   | Separate `quota.user.package_bytes` and `quota.org.package_bytes`, default `0` (off). Usage counts each distinct blob once per owner. A push over it is `DENIED`.                                                                                                                                                                            |
| 7   | Deleting a tag keeps its manifest as an untagged manifest. Deleting a manifest deletes its tags. An hourly sweep deletes unreferenced blob objects after a 24 h grace. No auto-deletion of untagged manifests; no retention policies.                                                                                                          |
| 8   | Acceptance is a real `docker login`, push and pull on a throwaway server, plus the OCI conformance suite run locally, results in the final PR body.                                                                                                                                                                                          |
| 9   | A transfer leaves the path alone; the package page notes the owner mismatch, and a manager of the linked Repository may re-link to any Repository they manage. A Repository rename changes nothing because the path has no repo name; an owner (user or organization) rename breaks pulls under the old name and is documented, owner aliases being future work. A package follows its linked Repository on purge; deleting the path's owner purges everything under it.                                                                                                                                                                                                     |
| 10  | `CanRead` is checked on every request, never cached publicly. A private or soft-deleted Repository answers the same `404` as a missing one.                                                                                                                                                                                                  |
| 11  | A separate `registry` rate-limit resource with generous defaults. `/v2/token` counts as `core`.                                                                                                                                                                                                                                              |
| 12  | A manifest `GET` (not `HEAD`, not blobs), by tag or by digest, counts one download on the manifest and on the tag used. Counts are batched in memory and flushed.                                                                                                                                                                              |
| 13  | The package page's readme card renders the linked Repository's default-branch README.                                                                                                                                                                                                                                                         |
| 14  | Tags and versions are deletable with `CanWrite`. Deleting a whole package needs `CanManage` and a typed name.                                                                                                                                                                                                                                |
| 15  | Generic files go to `PUT /api/repos/{owner}/{repo}/packages/generic/{name}/{version}/{filename}` (so target-restricted tokens and the scope allow-list apply) and download from `/{owner}/{repo}/packages/generic/{name}/{version}/{filename}`, with Bearer or Basic (PAT) auth. sha256 per file, 512 MiB cap, re-upload is `409`.                                                                                                                                             |
| 16  | Manifests with a `subject` are accepted and `GET /v2/{name}/referrers/{digest}` is served from the database. Referrer manifests are hidden from the tag list.                                                                                                                                                                                |
| 17  | `GET /v2/_catalog` is `404`. `tags/list` on an unreadable package is `404`.                                                                                                                                                                                                                                                                  |
| 18  | `registry.enabled` (default true; false makes `/v2` answer `404`). The token realm is built from `server.base_url`. A reverse proxy must pass `/v2/` with no body limit and no buffering.                                                                                                                                                     |
| 19  | One PR for the spec, built on an integration branch from nine tickets. |
| 20  | A blob can arrive before its package is linked, so the first blob upload creates a pending package (no Repository, visible to nobody, continued only by its creator); the first manifest push links it; one never linked expires with the upload TTL. Blob links belong to the package. A new name needs write access to at least one Repository of the owner. |
| 21  | The `packages:*` scopes are for personal access tokens (and device login), like `repo:admin`; OAuth apps cannot request them. |
| 22  | The registry JWT carries its own `aud` and `typ`, is never accepted as a session, and may outlive a revoked PAT by its 5 minutes (accepted).                                                                                                                                                                                                                                                                       |

## User Stories

1. As a developer, I want to `docker login` to my Instance with a personal access token, so that I do not need a second
   credential.
2. As a developer, I want `docker push host/owner/name:tag` to store my image, so that the image lives next to its code.
3. As a developer, I want a pushed image linked to my Repository from its `org.opencontainers.image.source` label, so
   that I choose the Repository without a second step.
4. As a developer, I want an image named like a Repository to link to it with no label, so that the simplest setup works.
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
13. As a user with only `repo:write`, I want a clear `403` naming the missing `packages:*` scope, so that I know what to
    add to my token.
14. As a user without access to a private Repository, I want the same `404` for its images as for a missing one, so
    that private names do not leak.
15. As a Repository writer, I want to delete a tag, so that I can retire a version.
16. As a Repository writer, I want an untagged manifest listed and deletable, so that I can see what a re-push left
    behind.
17. As a Repository admin, I want to delete a whole package after typing its name, so that a writer cannot wipe it by
    accident.
18. As a manager of a package's Repository, I want to re-link it to another Repository I manage, so that a mistaken link
    or a transfer can be fixed.
19. As a Repository owner, I want the package to follow the Repository's visibility at once, so that making it private
    stops anonymous pulls.
20. As a Repository owner, I want a soft-deleted Repository's packages hidden and restored with it, so that Restore
    loses nothing.
21. As an owner who transfers a Repository, I want existing `docker pull` commands to keep working, so that consumers do
    not break.
22. As a reader of a package whose Repository moved to another owner, I want a note on the page saying so, so that I
    understand its path.
23. As a release engineer, I want to upload a generic file with `curl -T` and a token, so that CI can publish a tarball.
24. As a release engineer, I want several files under one version with a sha256 each, so that I can verify downloads.
25. As a release engineer, I want a re-upload of the same file refused, so that a published version never changes
    silently.
26. As a consumer, I want to download a generic file by URL, so that `curl -LO` is the install.
27. As a user of cosign or another signing tool, I want signatures stored next to an image through the referrers API, so
    that tools work without junk tags.
28. As a Repository reader, I want the Packages tab to list packages with type, latest version, downloads and updated
    time, so that I can find the right one.
29. As a Repository reader, I want to filter the list by type and name, so that a long list is manageable.
30. As a Repository reader, I want a package page with a copyable `docker pull` command, so that I need no manual
    typing.
31. As a Repository reader, I want a tags table with digest, size and publisher, so that I see exactly what a tag points
    at.
32. As a Repository reader, I want the supported platforms of a multi-platform tag, so that I know it fits my machine.
33. As a Repository reader, I want the Repository's README on the package page, so that usage notes are one click away.
34. As a profile visitor, I want an owner's Packages tab across their Repositories, so that I can browse an
    organization's images.
35. As an organization owner, I want the same tab on the organization profile, so that packages show for teams.
36. As an owner, I want my package storage and its quota shown in account settings, so that I see what I use.
37. As an operator, I want `quota.*.package_bytes` off by default, so that upgrading changes nothing.
38. As an owner at the quota, I want a push refused with a message naming usage and quota, so that I know what to
    delete.
39. As an operator, I want unreferenced blobs swept automatically after a grace period, so that deleted images free
    disk.
40. As an operator, I want an in-flight push never to lose its layers to the sweep, so that GC cannot corrupt an image.
41. As an operator, I want the registry on S3 as well as local disk, so that it follows my storage choice.
42. As an operator, I want `registry.enabled: false` to remove the whole `/v2` surface, so that I can run without it.
43. As an operator, I want a separate rate-limit bucket for the registry, so that a pull of a large image does not
    exhaust the web budget.
44. As an operator, I want an abandoned upload session to expire and free its temp file, so that disk does not fill.
45. As an operator, I want docs for the reverse proxy settings `/v2/` needs, so that pushes do not fail at the proxy.
46. As a Cloudzilla user, I want `cz auth login --scope packages:write` to mint a registry token, so that I can paste it
    into `docker login`.
47. As a security reviewer, I want a token without `packages:*` to be refused on `/v2`, so that a Git-only token cannot
    publish.
48. As a security reviewer, I want blob access decided by link rows, so that knowing a digest never reads another
    Repository's layer.
49. As a security reviewer, I want mount to need read access to the source Repository, so that mount cannot copy a
    private layer.

## Implementation Decisions

- **Modules.** A `RegistryService` (token issue and verify, upload sessions, blobs, manifests, tags, referrers, delete,
  GC), a `PackageService` (generic packages, package list and detail, deletion, linking, quota and usage), stores for
  both, and handlers `registry_handler.go` (the `/v2/` wire protocol, which writes OCI error JSON, not the app's
  `writeJSON` errors), `package_handler.go` and `package_page_handler.go`. Handlers call services only.
- **Schema (migration 142 onward; re-check origin/main).** A `packages` row per (owner, type, name) pointing at its
  Repository. Registry blob rows (digest, size, `unlinked_at`) plus per-package link rows (a package may be pending, with no
  Repository, until its first manifest push). Manifest rows per package (digest, media
  type, size, exact bytes, subject digest, artifact type, platform summary, pull count) with child and blob reference
  rows so GC and the platform display need no JSON parsing. Tag rows (name, manifest, pusher, pull count). Upload-session
  rows (id, repository, user, size so far, expiry). Generic version and file rows (name, size, sha256, key). Migration
  numbers go to the ticket that needs them.
- **Linking.** Docker pushes blobs before the manifest, so the first blob upload to a new name creates a pending package
  (no Repository, visible to nobody, usable only by its creator) and the first manifest push links it. The label route
  needs `CanManage` on the target, the name-match route `CanWrite`. A manager of the linked Repository may re-link to
  any Repository they manage. A pending package never linked expires with the upload TTL.
- **Names.** The OCI name regex, lowercase only, at most 255 characters with host; the owner segment is matched
  case-insensitively, and an owner that can never be a valid path (a trailing separator) gets a clear refusal. A name may
  contain segments called `blobs`, `manifests`, `tags` or `referrers`, so one `/v2/*` handler parses the route from the
  right. `v2` and `packages` join `reservedOwnerNames`, and startup logs any existing account that already has one.
- **Auth.** `GET /v2/` answers `401` with `WWW-Authenticate: Bearer realm="<base_url>/v2/token",service="…"`. The
  token endpoint accepts HTTP Basic with a `czp_` personal access token only, never an account password, and refuses a
  key-bound token, a suspended user and a token whose `Targets` do not cover the Repository. It checks the requested
  `repository:<name>:<actions>` scope against `CanRead`/`CanWrite` on the linked Repository and returns only the
  intersection, signed with an HMAC key derived from `auth.jwt_secret` with a purpose label, so it works without
  `security.secret_key`. Claims carry `aud` and `typ: registry`, and a registry token is never accepted as a session. An
  anonymous request gets pull on public Repositories only. Every `/v2` request without a sufficient token is `401` with
  `WWW-Authenticate` naming `scope` and `error`, and `Docker-Distribution-API-Version` is sent; a token whose
  credentials were accepted but which lacks the `packages:*` scope is `403` (`DENIED`).
- **Scopes.** `packages:read` admits pull; `packages:write` admits push and delete. Both are PAT and device-login scopes only, like
  `repo:admin`. `packages` joins the repo content resources of the scope allow-list for the generic API; `/v2` has its own
  JWT check and never passes through that allow-list.
- **Uploads.** `POST` starts a session; `PATCH` appends to a temp file in a named temp directory cleaned at startup, with `Content-Range` optional (Docker usually omits it) and checked when sent; the session is bound to its user and package; the final `PUT` (or a
  monolithic `PUT`/`POST ?digest=`) verifies the digest and size, puts the object with its exact size, then writes the
  link row. Cross-repository mount copies only a link row, after `CanRead` on the source and a link row there. The
  object is written before the row, as for release assets. In-flight bytes count against the quota, each user has a cap on open sessions, and the handlers lift the server's read and write deadlines. Blob `Range` is served by skipping bytes in the stream, because `Backend` has no ranged read (cost accepted).
- **Manifests.** Stored as exact bytes, up to 4 MiB. A manifest whose blobs or children are not linked to the package
  is `MANIFEST_BLOB_UNKNOWN`. Both OCI and Docker v2 media types are accepted. `PUT …?tag=` multi-tag is not required.
  Referrer manifests (with `subject`) appear in referrers and not in tags. Tag list is paginated by `n` and `last`.
- **Delete.** Manifest delete by digest removes its tags and reference rows; tag delete removes the tag only. A blob
  delete endpoint is accepted and only removes the link row.
- **GC.** One sweep service, hourly, in `main.go` next to the log and attachment sweeps: blobs unlinked for longer than
  `registry.gc_grace` are selected with one guarded statement that re-checks no link exists (linking takes a share lock on the blob row), and the object is deleted before the row so a failed delete is retried, in batches of 500; expired upload sessions and their temp files go
  with it. `RepoService.PurgeExpired` and user or organization deletion collect the package rows and objects the way
  release assets do.
- **Quota.** `package_bytes` is checked at blob commit, manifest push and generic upload, and in-flight upload bytes count. Usage is the sum of distinct blob sizes
  linked from the owner's Repositories plus generic file sizes.
- **Download counts.** Counted in memory per manifest and tag, flushed on a timer and on shutdown.
- **Middleware.** `/v2/` and generic upload are streamed uploads: `IsStreamedUpload` and CSRF's exemption cover them (Bearer and Basic), and `RepoAlias` skips `v2`. The `registry` rate-limit bucket keys on the registry JWT's subject, not the client IP. `/v2` is not added to the setup path check: before setup it redirects to `/setup`, as git transport does.
- **Generic.** Upload streams to a temp file while hashing, then puts the object. Responses carry
  `Content-Disposition: attachment`, `nosniff`, `Content-Security-Policy: default-src 'none'; sandbox`, and are never
  cached publicly.
- **UI.** `Packages` is a repository tab after Releases and a tab on the owner profile. Pages follow the three standing
  mocks, use the existing table, tabs, badge, dialog and copy-button components, and render the README through the
  same Markdown path as the code tab.
- **Config.** `registry.enabled`, `registry.max_blob_bytes`, `registry.upload_ttl`, `registry.gc_grace`,
  `packages.max_file_bytes`, `quota.{user,org}.package_bytes`, `rate_limit.registry.{anonymous,authenticated}`.
- **Docs.** A new `docs/package-registry.md`, a line in CLAUDE.md's subsystem list, updates to `access-control.md`
  (scopes, routes), `storage.md` (layout, cleanup), `configuration.md`, `api-reference.md`, `cli.md` (scopes), `ci.md`
  (the secrets recipe) and ROADMAP status.

## Testing Decisions

- **Seam:** the existing highest seam, router tests over a real database and the local `Backend` in a temp dir
  (`newVerificationRouter`, `internal/router/fork_test.go`). A small in-test OCI client drives the wire protocol: token,
  chunked push, manifest push, pull, tag list, referrers, delete. S3 runs behind the existing `TEST_S3_*` variables.
- A good test asserts external behaviour: status codes, OCI error codes, headers (`Location`, `Docker-Content-Digest`,
  `WWW-Authenticate`), the bytes pulled, and who sees what. It does not assert on key names or table layout.
- **Service tests** for the sweep (an injected clock, and a hook between its read and its delete to race a push), quota arithmetic and the link rules. Token and session expiry use the same injected clock. Memory bounds are tested with a counting reader that fails if more than a fixed window is ever buffered.
- **Cases:** anonymous pull of public and private, a reader pushing, a token with the wrong scope, a Repository made
  private mid-session, a soft-deleted Repository, a rename and a transfer, mount of an unreadable source, a digest
  mismatch, a chunk out of order, an expired session, a duplicate generic upload, quota reached, a sweep racing a push.
- **Gate:** a real `docker login`, push and pull on a throwaway server (scratch Postgres, unique
  `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), and the OCI conformance suite with the API toggles
  for pull, push, tags, referrers and delete, results in the final PR body. `podman` is documented, not tested.
- **Prior art:** `internal/router/fork_test.go`, `release_asset_service_test.go`, `workflow_log_service` sweep tests, the
  storage tests, the throwaway-server notes for live checks (light and dark at 1440 for the UI).

## Out of Scope

- npm, PyPI, Go module proxy, Maven, NuGet, Helm, and every other ecosystem.
- A registry token for CI Jobs, Docker-in-Job, and any `cz` registry command.
- `GET /v2/_catalog`, cleanup and retention policies, per-package visibility, and a metadata-database style online GC.
- Signature display in the UI, vulnerability scanning, and mirroring or proxying another registry.
- Release asset enhancements (ROADMAP 16.3).
- Podman automated tests.
- Owner aliases, so an owner rename breaks existing pull commands.
- OAuth-app access to `packages:*`.

## Further Notes

- The registry runs in the same process and port as the app. A proxy that buffers request bodies or limits their size
  breaks layer pushes.
- Docker accepts plain HTTP only for `localhost`; elsewhere the Instance needs HTTPS.
- Server restarts lose in-flight uploads; clients retry. Pull counts batched in memory are lost on a crash, and the registry assumes one instance (local temp files, in-memory counts). These costs are accepted.
- The mock's open questions are answered by decisions 2, 3, 6, 7, 12 and 13.

# Package registry

The Instance is also a **Registry**: Docker and podman push and pull container images over the OCI Distribution v2 API
at `/v2/`, and a writer can upload generic files with a plain HTTP `PUT`. Every **Package** lives at `{owner}/{name}`
and is linked to one Repository, from which it takes its permissions: readers pull, writers push and delete, and there
is no per-package visibility. Layers and files live on the storage [`Backend`](./storage.md), so local disk and S3 both
work. The code is `RegistryService` (the wire protocol), `RegistryGCService` (the sweep) and `PackageService` (generic
files, lists, deletion, re-linking, quota), behind `registry_handler.go`, `package_handler.go` and
`package_page_handler.go`.

Related pages: [access-control](./access-control.md#container-registry-v2) (routes, scopes, status codes),
[storage](./storage.md#registry-blobs) (object layout, orphans), [api-reference](./api-reference.md#packages) (the JSON
and generic-file API), [configuration](./configuration.md) (every key and `CZ_*` variable), [cli](./cli.md#packages)
(minting a token with `cz`), [ci](./ci.md#publishing-a-generic-package-from-a-job) (the secrets recipe) and
[organizations](./organizations.md) (a rename breaks pulls).

## Path and linking

An image is `{host}/{owner}/{name}`: no repository segment, lowercase, `/` inside the name allowed
(`git.example.com/ada/app/worker`). The **path owner** is the first segment, a user or an organization, resolved
ignoring case. A name may contain segments called `blobs`, `manifests`, `tags` or `referrers`, so `/v2/*` is parsed from
the right. `v2` is a reserved owner name; startup logs an existing account called `v2`.

Docker uploads layers before the manifest, so the first upload to a new name creates a **pending package**: no
Repository, visible to nobody else, usable only by its creator, at most `registry.max_pending_per_user` per user, and
gone `registry.upload_ttl` after creation whatever happens. A new name needs `CanWrite` on at least one Repository of
the path owner.

The first manifest push **links** the package to a Repository, in this order:

1. A Repository of the path owner named like the image's first segment (`ada/app` and `ada/app/worker` both match a
   Repository `app`). It wins whatever the image says, and needs `CanWrite`; without it the push is `403`.
2. Otherwise the image config's `org.opencontainers.image.source` label names a Repository of the path owner
   (`owner/repo` or a URL on `server.base_url`; a `.git` suffix, a trailing `/` and the host's case are ignored, another
   host is ignored). The caller needs `CanManage` there. The config is read only for this, capped at 1 MiB.
3. Otherwise the push is refused with a message naming both routes, which Docker prints as `error from registry: …`.

A name-matched push never reads the label, so a label cannot claim an image that matches a Repository. Later pushes need
`CanWrite` on the linked Repository.

## Tokens, scopes and `docker login`

Docker logs in with a **personal access token** (`czp_…`), never an account password: a password would bypass TOTP,
suspension and SSO, so the registry answers it `401`. Two scopes exist for tokens and device logins only (an OAuth app
cannot be granted them): `packages:read` pulls and downloads; `packages:write` also pushes, uploads and deletes. A
`repo:*` scope never admits a package route and a package scope admits nothing else. A key-bound token and a suspended
user are refused.

```sh
cz auth login --host https://git.example.com --scope packages:write --insecure-storage   # or create a PAT in Settings
docker login git.example.com -u <username> --password-stdin   # paste the token
docker push git.example.com/ada/app:v0.4.1
```

`docker login` sends the token to `GET /v2/token`, which answers a five-minute **registry token** (an HMAC JWT keyed
from `auth.jwt_secret` and a purpose label, so it works without `security.secret_key`; `typ: registry`, never accepted
as a session). It may outlive a revoked PAT by those five minutes. Its access list is what the caller asked for,
intersected with what `CanRead` (`pull`) and `CanWrite` (`push`, `delete`) allow on the linked Repository, capped by the
PAT's scope. Anonymous callers get `pull` on public Repositories only. `GET /v2/_catalog` is `404`. Docker's credential
store keeps one login per registry host (a second `docker login` to the same host replaces the first, whatever
`DOCKER_CONFIG` says when the store is the macOS keychain), so to test two identities on one machine give the server two
host names (`localhost` and `127.0.0.1`).

CI gets no registry token: the `czj_` job token only fetches Git data and a Job container has no Docker daemon. A
workflow uses a `packages:write` PAT held in `secrets.*` with `curl`, `crane` or `skopeo`; see
[the recipe](./ci.md#publishing-a-generic-package-from-a-job). Podman uses the same `/v2/` protocol and is documented,
not tested.

## Status codes

| Where       | Case                                                                       | Answer                                                        |
| ----------- | -------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `/v2/token` | A wrong or missing PAT for a non-public scope, or an account password      | `401` with `WWW-Authenticate: Basic realm="registry"`         |
| `/v2/token` | A valid PAT without `packages:*`, a key-bound PAT, a suspended user        | `403` `DENIED` naming the reason                              |
| `/v2/token` | Valid credentials, no access to the requested name                         | `200` with that name missing from the access list             |
| `/v2/*`     | No token, an expired or tampered one                                       | `401` with `Bearer realm,service,error="invalid_token"`       |
| `/v2/*`     | An access list without the action the request needs                        | `401` with `scope` and `error="insufficient_scope"`           |
| `/v2/*`     | The token grants the action but the name is unknown, pending or unreadable | `404` `NAME_UNKNOWN` (a referrers `GET` answers `200`, empty) |
| `/v2/*`     | Quota reached                                                              | `403` `DENIED` naming usage and quota                         |
| `/v2/*`     | A blob still named by a manifest, an index child                           | `409` `DENIED`                                                |
| `/v2/*`     | A refused link                                                             | `403` `DENIED` naming both ways to link                       |
| `/v2/*`     | Too many open sessions, bytes in flight or pending packages                | `429` `TOOMANYREQUESTS`                                       |
| `/v2/*`     | A blob over `registry.max_blob_bytes`; a manifest over 4 MiB               | `413` `SIZE_INVALID`                                          |
| `/v2/*`     | A digest that is not `sha256:<64 hex>` (sha512 is not supported)           | `400` `DIGEST_INVALID`                                        |
| Generic     | A Basic PAT without the scope                                              | `403`; unreadable or missing is `404`                         |

A token with an empty access list sees `401` for every name, public or private, existing or not, so private names do not
leak, and a reader who tries to push sees the same "unauthorized" Docker shows for a wrong token. What Docker prints:

| Situation                              | Docker says                                                                    |
| -------------------------------------- | ------------------------------------------------------------------------------ |
| Login with a `repo:write`-only token   | `denied: the token needs the packages:read or packages:write scope`            |
| Login with a wrong token or a password | `unauthorized`                                                                 |
| Pull of a private or unknown name      | `pull access denied … repository does not exist or may require 'docker login'` |
| Push by a reader or a stranger         | `push access denied …` or `insufficient_scope: authorization failed`           |
| Push of an image that cannot be linked | `error from registry: this image is not linked to a repository: …`             |

## Permissions, transfers and rename

`CanRead` on the linked Repository is checked on every request and the answer is never cached publicly, so making a
Repository private stops anonymous pulls at once, and a soft-deleted Repository's packages disappear and come back on
restore. Readers pull; writers push and delete tags, manifests and generic versions; deleting a whole package needs
`CanManage` on the linked Repository, or being the path owner, and a typed name in the UI. A mount from another package
needs `CanRead` on the source.

- A Repository **rename** changes nothing: the path has no repository name.
- A **transfer** leaves the package at its path, linked to the transferred Repository, so pulls keep working; the
  package page notes the mismatch. The path owner may always delete a package under its path or re-link it to one of its
  own Repositories (never to another owner's).
- An **owner rename breaks pulls** under the old name: the path is the owner's name and there are no owner aliases. Tell
  consumers before renaming a user or organization that publishes images.
- Deleting the path owner purges everything under the path; purging a Repository purges only the packages linked to it.
  Rows go at once, objects through the sweep.

## Quotas and limits

`quota.user.package_bytes` and `quota.org.package_bytes` (default `0`, off) cap package storage per owner, separate from
the git storage quota. The path owner pays: each distinct blob linked from its packages once (pending ones included),
the generic files, and the bytes of open upload sessions. A superadmin's personal account is exempt. Reaching the quota
exactly is allowed; going over is `403`.

| Key                                    | Default | What it bounds                                                |
| -------------------------------------- | ------- | ------------------------------------------------------------- |
| `registry.enabled`                     | `true`  | `false` makes `/v2` answer `404`                              |
| `registry.max_blob_bytes`              | 5 GiB   | One blob                                                      |
| `registry.max_sessions_per_user`       | 10      | Open upload sessions per user                                 |
| `registry.max_inflight_bytes_per_user` | 10 GiB  | Bytes in a user's open sessions                               |
| `registry.max_pending_per_user`        | 20      | Pending packages per user                                     |
| `registry.upload_ttl`                  | 24h     | Session and pending package lifetime, from creation           |
| `registry.gc_grace`                    | 24h     | How long an unreferenced link, then an unlinked blob, is kept |
| `packages.max_file_bytes`              | 512 MiB | One generic file                                              |

`/v2/*` has its own rate-limit resource, `registry` (2000 anonymous per client IP, 20000 signed in per registry-token
user, per window); `/v2/token` counts as `core`. A Basic PAT on the generic paths is keyed per user. A zero or negative
limit, TTL or grace stops the server at startup. A cap on open sessions bites tools that start uploads they never finish
(the conformance suite starts dozens); raise `registry.max_sessions_per_user` for such a run.

## Garbage collection and orphans

Deleting a tag, manifest or package, purging a Repository and deleting an account remove **rows only**; no object is
deleted inline. Blobs are content-addressed, one object per digest shared by every package, and a package's link rows,
never the object's existence, grant access.

`RegistryGCService.Sweep` runs at startup and then hourly, in batches of 500:

1. drops links no manifest of the package names that are older than `registry.gc_grace`, and expired pending packages
   and upload sessions with their spool files;
2. stamps `unlinked_at` on blobs with no link left;
3. per blob stamped more than a grace ago, locks the row (`FOR UPDATE SKIP LOCKED`), re-checks that no link exists,
   deletes the object, then the row.

So a deleted image's private layers go after about two graces, and a layer another image still links is never touched.
Linking takes `FOR SHARE` on the blob row and re-puts the object if the row is gone, so a push racing the sweep either
waits or succeeds. A failed object delete rolls that blob back and the next run retries it.

An **orphan** is an object under `registry/` whose `registry_blobs` row is gone, or a file under `packages/generic/`
that no `package_files` row names (a failed upload, a purge that could not delete the object): safe to delete. Download
counts are kept in memory, flushed every minute and at shutdown; a crash loses the batch.

## Generic packages

```sh
curl -u ci:$TOKEN -T app-1.0.tar.gz https://git.example.com/api/repos/ada/app/packages/generic/app/1.0/app-1.0.tar.gz
curl -u ci:$TOKEN -LO https://git.example.com/ada/app/packages/generic/app/1.0/app-1.0.tar.gz
```

`PUT /api/repos/{owner}/{repo}/packages/generic/{name}/{version}/{filename}` needs `CanWrite` and `packages:write`,
Bearer or Basic; the response carries the file's `sha256`. Names outside `[A-Za-z0-9._-]`, `.` and `..` are `400`, never
cleaned; a re-upload of the same file is `409` (a published version never changes); a name already used by another
Repository of the owner is `409`; over `packages.max_file_bytes` is `413`. Downloads run `CanRead` every time and carry
`Content-Disposition: attachment`, `nosniff` and a sandboxing `Content-Security-Policy`. Routes and bodies are in
[api-reference](./api-reference.md#packages).

## Operating it

- **Reverse proxy.** Pass `/v2/` through unchanged, with **no request body limit and no buffering**: a layer is one
  large `PATCH` or `PUT`, and a proxy that buffers or caps it breaks pushes. For nginx: `client_max_body_size 0;`,
  `proxy_request_buffering off;`, `proxy_buffering off;` and a read timeout longer than your slowest push. The server
  lifts its own read and write deadlines for these routes. Set `server.trusted_proxies` so rate limits see client IPs.
- **HTTPS.** Docker accepts plain HTTP only for `localhost` and `127.0.0.0/8`; any other host needs HTTPS (or an
  `insecure-registries` entry on every client). The token realm and `aud` come from `server.base_url`, so it must be the
  public URL clients use.
- **Turning it off.** `registry.enabled: false` makes the whole `/v2` tree `404`; generic files stay. The sweep still
  runs. Before setup completes `/v2` redirects to `/setup`, as Git transport does.
- **One instance.** Upload spool files are local and pull counts are in memory, so run one process. The spool is
  `os.TempDir()/cloudzilla-registry-spool`, emptied at startup; a restart loses in-flight uploads and clients start
  again. It needs room for `registry.max_inflight_bytes_per_user` per active pusher.
- **Not implemented.** `GET /v2/_catalog`, `PUT …?tag=` multi-tag, sha512 digests, retention policies, other ecosystems
  (npm, PyPI, Maven), signature display and scanning. Referrers are served from the database with `artifactType`
  filtering; an unknown subject or name is an empty index, never `404`.

## Checking a build

The real-client bar is `docker login`, push and pull by tag and digest, a multi-platform image, a public and a private
Repository, an anonymous pull and a refused push on a throwaway server. For the OCI conformance suite
(`github.com/opencontainers/distribution-spec/conformance`), create a user `conformance` with Repositories `repo1` and
`repo2`, mint a `packages:write` token for it, raise `registry.max_sessions_per_user`, and run it with `OCI_REGISTRY`,
`OCI_TLS=disabled`, `OCI_REPO1=conformance/repo1`, `OCI_REPO2=conformance/repo2`, `OCI_USERNAME` and `OCI_PASSWORD` set,
`OCI_DATA_SHA512=false` and the default API toggles. It writes `report.html` and `junit.xml`. Docker Desktop's daemon
cannot reach a server on the host's `127.0.0.1`: run the server in a container published on a port, with
`server.base_url` set to `http://localhost:<port>`.

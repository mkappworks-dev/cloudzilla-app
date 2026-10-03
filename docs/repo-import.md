# Repository import

`/repos/import` copies a Git repository from another host into a new Cloudzilla repository: every branch and tag, with HEAD on the source's default branch. Issues, pull requests, wikis, releases and LFS objects are not imported, and the copy does not track the source afterwards.

## Flow

1. `POST /api/imports` validates the request and returns `202` with the job ID. The form then opens `/repos/import/{id}`.
2. `ImportService` runs the job in a goroutine, at most 3 at once server-wide; later jobs wait as `queued`. A user may have 5 jobs queued or running.
3. The clone goes into `<git.repos_root>/.import-tmp/<job id>`: only `refs/heads/*` and `refs/tags/*` are fetched, and the `origin` remote is removed afterwards.
4. `RepoService.CreateFromImport` re-checks the user's right to the owner, claims the name, inserts the row and renames the clone into place.
5. The status page polls every 2 s and redirects to the repository when the job is done.

Jobs live in memory and are dropped an hour after they finish. A restart loses imports in flight; startup deletes `.import-tmp`.

## Security

- Only `http` and `https` URLs are accepted. go-git treats a bare path or `file://` URL as a repository on the server's own disk.
- Credentials go in the username and token fields, never the URL. They are used for one clone as HTTP basic auth and are not stored or logged.
- The go-git HTTP client is process-wide. When a request's context carries an import guard, the dialer resolves the host itself and refuses loopback, private, link-local, multicast, unspecified, `0.0.0.0/8` and `100.64.0.0/10` addresses, then connects to the vetted IP. Redirects re-dial through the same check. `import.allow_local_networks: true` turns the check off, for importing from a server on your own network.
- Imports ignore `HTTP(S)_PROXY`, and response bytes are capped by `git.max_pack_bytes`.
- Each accepted import (202) is written to the audit log as `repo.import` with the source URL, whether or not the clone later succeeds.
- Personal access tokens and OAuth-app tokens are refused on `/api/imports`; only a session (the cookie, or the login JWT as Bearer) can start or read an import. The owner is in the request body, which the token-target check (based on the URL path) can't see.

## Configuration

| Key | Default | Meaning |
| --- | --- | --- |
| `import.allow_local_networks` | `false` | Allow sources on private networks |
| `import.timeout` | `30m` | Time limit for one import |
| `git.max_pack_bytes` | 2 GiB | Also caps the bytes one import downloads |

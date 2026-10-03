# Repository import

`/repos/import` copies a Git repository from another host into a new Cloudzilla repository: every branch and tag, with HEAD on the source's default branch. Issues, pull requests, wikis, releases and LFS objects are not imported, and the copy does not track the source afterwards.

## Flow

1. `POST /api/imports` validates the request and returns `202` with the job ID. The form then opens `/repos/import/{id}`.
2. `ImportService` runs the job in a goroutine, at most 3 at once server-wide; later jobs wait as `queued`. A user may have 5 jobs queued or running.
3. The clone goes into `<git.repos_root>/.import-tmp/<job id>`: only `refs/heads/*` and `refs/tags/*` are fetched, and the `origin` remote is removed afterwards.
4. `RepoService.CreateFromImport` re-checks the user's right to the owner, claims the name, inserts the row and renames the clone into place.
5. The status page polls every 2 s and redirects to the repository when the job is done.

Jobs live in memory and are dropped an hour after they finish; a user keeps at most 20 finished jobs, and starting another import evicts the oldest. A restart loses imports in flight; startup deletes `.import-tmp`.

## Security

- Only `http` and `https` URLs are accepted. go-git treats a bare path or `file://` URL as a repository on the server's own disk.
- A URL longer than 2048 bytes, or with a query string, is refused. go-git appends `/info/refs` after the query, so such a URL could never clone, and a `?token=` secret in it would reach the audit log, the logs and the status page. The job and its audit row keep the URL, so the length is bounded.
- Credentials go in the username and token fields, never the URL. They are used for one clone as HTTP basic auth and are not stored or logged.
- The go-git HTTP client is process-wide. When a request's context carries an import guard, the dialer resolves the host itself and refuses loopback, private, link-local, multicast, unspecified, `0.0.0.0/8` and `100.64.0.0/10` addresses, then connects to the vetted IP. Redirects re-dial through the same check. `import.allow_local_networks: true` turns the check off, for importing from a server on your own network.
- Imports ignore `HTTP(S)_PROXY`.
- Response bodies are capped by kind, so one hostile source can't make an import hold gigabytes in memory. The pack is capped at `git.max_pack_bytes`; the `info/refs` ref advertisement at 64 MiB; a non-2xx body is cut off at 64 KiB, since go-git only uses it as error text. The 64 MiB and 64 KiB caps apply even when `git.max_pack_bytes` is `0`. Crossing the pack or ref cap fails the import, and the message names the cap.
- Each accepted import (202) is written to the audit log as `repo.import` with the source URL, whether or not the clone later succeeds.
- Personal access tokens and OAuth-app tokens are refused on `/api/imports`; only a session (the cookie, or the login JWT as Bearer) can start or read an import. The owner is in the request body, which the token-target check (based on the URL path) can't see.

## Configuration

| Key | Default | Meaning |
| --- | --- | --- |
| `import.allow_local_networks` | `false` | Allow sources on private networks |
| `import.timeout` | `30m` | Time limit for one import |
| `git.max_pack_bytes` | 2 GiB | Also caps the pack one import downloads (`0` = no cap) |

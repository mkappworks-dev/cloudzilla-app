# Mirror settings card and API

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Blocked by: 05, 06, 07

Spec: [../spec.md](../spec.md#api)

## What

- A settings card in `repo_settings.templ` (`CanManage`) shows the URL, the username, the token as "set" or "not set", the interval, the last sync, the last error, Sync now, Save and Stop mirroring. Agree the layout with the maintainer before building it.
- API:
  - `GET`, `PATCH` and `DELETE /api/repos/{owner}/{repo}/mirror`
  - `POST …/mirror/sync`, which returns 202
  - The token is never returned. `PATCH` can replace or clear it.
- `mirror` is added to `repoAdminResources` in `middleware/scope.go`. `scope_test.go:58-59` currently uses `/mirror` as its unlisted example, so pick another path there.
- Stop mirroring deletes the row, and with it the credentials. The repo becomes writable.
- Audit: `repo.mirror.update` and `repo.mirror.delete`.

## Acceptance criteria

- [ ] `CanManage` is required for GET, PATCH and DELETE. `CanWrite` is required for sync.
- [ ] Scoped tokens need `repo:admin`.
- [ ] No response or rendered page contains the token.
- [ ] After Stop mirroring, a push succeeds and no mirror row remains.
- [ ] Changing the URL or interval reschedules the mirror.

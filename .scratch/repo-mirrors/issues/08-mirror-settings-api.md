# Mirror settings card and API

Created: 2026-10-06
Category: enhancement
Status: done
Blocked by: 05, 06, 07

Spec: [../spec.md](../spec.md#api)

## What

- A settings card in `repo_settings.templ` (`CanManage`) shows the URL, the username, the token as "set" or "not set", the interval, the last sync, the last error, Sync now, Save and Stop mirroring. The agreed layout is frame 2 of the [mockups](https://claude.ai/artifact/1qdQ3E9jfTCzmijzWhWRgq): its own "Mirror" section in the settings sidebar, with a status strip and the form. The token is write-only ("Stored · enter a new one to replace it") with a "Remove the stored token" checkbox. The interval uses the same fixed choices as the import form. "Stop mirroring" is a separate red card.
- API:
  - `GET`, `PATCH` and `DELETE /api/repos/{owner}/{repo}/mirror`
  - `POST …/mirror/sync`, which returns 202. It already exists from ticket 07; this ticket only opens it to scoped tokens.
  - The token is never returned. `PATCH` can replace or clear it.
- `mirror` is added to `repoAdminResources` in `middleware/scope.go`. `scope_test.go:58-59` currently uses `/mirror` as its unlisted example, so pick another path there.
- Stop mirroring deletes the row, and with it the credentials. The repo becomes writable.
- Audit: `repo.mirror.update` and `repo.mirror.delete`.

## Acceptance criteria

- [x] `CanManage` is required for GET, PATCH and DELETE. `CanWrite` is required for sync.
- [x] Scoped tokens need `repo:admin`.
- [x] No response or rendered page contains the token.
- [x] After Stop mirroring, a push succeeds and no mirror row remains.
- [x] Changing the URL or interval reschedules the mirror.

## Comments

**Claude, 2026-10-06:**
- `PATCH` takes JSON or the settings form. In both, an empty `auth_token` keeps the stored token and `clear_token` removes it.
- A new URL, username or token makes the mirror due at once and wakes the loop, so a fix shows within seconds. A new interval counts from the last sync.
- The `repo.mirror.update` audit entry lists the changed field names and the remote URL, never the token.

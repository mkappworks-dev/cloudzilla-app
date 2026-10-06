# Pull mirrors are read-only

Created: 2026-10-06
Category: enhancement
Status: done
Blocked by: 01, 03

Spec: [../spec.md](../spec.md#pull-mirrors)

## What

- `CheckContentWritable` also returns `ErrRepoMirror`.
- HTTP push gets `403 Repository is a mirror and is read-only.`; SSH push gets the same text on stderr. Web and JSON writes are refused as in ticket 01.
- `PullService.Create` refuses when the base repo is a pull mirror, with "Pull mirrors are read-only; open the pull request upstream." The New PR button is hidden.
- The UI hides the same controls as for archived repos (ticket 01) and the Add file menu.
- The repo header shows a banner: "Mirror of `<url>` · synced <relative time> · Sync now". "Sync now" is shown only to users with `CanWrite`. When `last_error` is set, the banner shows it. The agreed layout is option A of the [mockups](https://claude.ai/artifact/1qdQ3E9jfTCzmijzWhWRgq) (frames 3A): a strip above the header, like the archived banner, plus a "Mirror" badge. When the last sync failed, the strip turns red, shows the error and the next retry, and offers "Mirror settings" (to `CanManage`) and "Sync now".
- Issues, discussions, the wiki and forks keep working.

## Acceptance criteria

- [x] Every path from ticket 01, plus HTTP and SSH push, is refused on a mirror, with tests.
- [x] PR creation into a mirror is refused through both the web and the API.
- [x] Forking a mirror gives a writable repo that is not a mirror.
- [x] The banner renders with the URL and sync state. Write controls are hidden.

## Comments

**Claude, 2026-10-06:**
- `Repository.ContentReadOnly()` now covers mirrors too, so every control ticket 01 hid for archived repos hides for mirrors without new template code.
- Sync now's endpoint (`POST /api/repos/{owner}/{repo}/mirror/sync`, which needs `CanWrite`) moved here from ticket 08, so the banner's button works from the commit that adds it. Ticket 08 opens it to `repo:admin` tokens with the rest of the mirror API.
- An HTTP push refusal no longer ends with two newlines: `http.Error` adds its own.

# Pull mirrors are read-only

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Blocked by: 01, 03

Spec: [../spec.md](../spec.md#pull-mirrors)

## What

- `CheckContentWritable` also returns `ErrRepoMirror`.
- HTTP push gets `403 Repository is a mirror and is read-only.`; SSH push gets the same text on stderr. Web and JSON writes are refused as in ticket 01.
- `PullService.Create` refuses when the base repo is a pull mirror, with "Pull mirrors are read-only; open the pull request upstream." The New PR button is hidden.
- The UI hides the same controls as for archived repos (ticket 01) and the Add file menu.
- The repo header shows a banner: "Mirror of `<url>` · synced <relative time> · Sync now". "Sync now" is shown only to users with `CanWrite`. When `last_error` is set, the banner shows it. Agree the layout with the maintainer before building it.
- Issues, discussions, the wiki and forks keep working.

## Acceptance criteria

- [ ] Every path from ticket 01, plus HTTP and SSH push, is refused on a mirror, with tests.
- [ ] PR creation into a mirror is refused through both the web and the API.
- [ ] Forking a mirror gives a writable repo that is not a mirror.
- [ ] The banner renders with the URL and sync state. Write controls are hidden.

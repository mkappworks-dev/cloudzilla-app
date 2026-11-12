# New-release form uploads

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 04

| Branch | Worktree | PR title | Effort | Score gain |
| --- | --- | --- | --- | --- |
| `feat/release-new-uploads` | `.worktrees/feat+release-new-uploads` | `feat(releases): attach files from the new-release form` | M | Features +0.1, Edge +0.1 |

Spec: [spec.md](../spec.md#release-assets).

## Problem

The new-release form describes downloadable assets but has no way to add one, and the files can't be sent with the
create request because the release has no id yet.

## Scope

- A file drop zone and picker on `release_new.templ` listing queued files with name and size, remove per file, and the
  limits from ticket 04 enforced client-side for early feedback.
- On Publish or Save draft: create the release, then upload each queued file in turn to ticket 04's endpoint with
  per-file progress, then go to the detail page. A failed upload leaves the release in place, shows which files failed
  and why, and links to the detail page where "Add asset" retries.
- Keep the form's typed values on a failed create (ticket 11 mechanism) and show inline errors with ticket 33's
  pattern. Remove the "assets" claim from copy only if the feature is hidden; otherwise keep it.
- Script lives in `cmd/server/frontend/` and is referenced with `assets.URL`; new Tailwind classes go in the templ so
  `@source` sees them.

## Files

- `internal/view/pages/release_new.templ`, `cmd/server/frontend/` (upload script)
- `docs/api-reference.md` only if ticket 04 left a gap

## Acceptance criteria

- [x] Files picked or dropped on the form upload after the release is created, with progress per file
- [x] A failed upload leaves a usable release and a clear route to retry
- [x] Draft and publish both upload
- [x] Keyboard-operable picker and queue, announced progress and errors
- [x] Mocks updated, light and dark

## Mock updates

`mockups/release_new.html`: empty drop zone, queued files, uploading, partial failure. Light and dark at 1440.

## Verification

Throwaway server per the harness: publish and draft with several files, one over the limit, a mid-upload failure
(stop the server or block the request), then retry from the detail page. Light and dark at 1440.

## Comments

Claude, 2026-11-12: Built as scoped. The form now creates the release with `fetch` (JSON) instead of htmx, so it keeps the
page, can read the new release's id, and shows tag errors under the Tag field (`CreateRelease` answers the JSON path's
tag errors with `field: "tag_name"`). A form with no files still goes to the releases list; with files it goes to the
detail page once every upload succeeds. After a partial failure the form locks (no second create) and links to the
detail page. Limits come from the same config as the detail page. Mock states: `mockups/release_new.html?state=queued |
uploading | partial`.

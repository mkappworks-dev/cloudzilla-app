# Time out card save requests after 30 seconds

Created: 2026-10-10
Category: bug
Status: done

## Problem

`send()` in `cmd/server/frontend/static/kanban.js` has no timeout. A save that never settles keeps a field's `saving` flag true, so the card modal's close X stays disabled and Escape is swallowed (`closeBlocked`) until the user reloads and loses any unsaved in-place edit.

## Scope

- `send()` aborts after 30 s (`AbortSignal.timeout`, with an `AbortController` fallback), the value in one named constant.
- The abort takes the existing error path: the field shows an error, restores its previous value, and `saving`/`busy`/`inflight` clear so the X and Escape work again.
- Every `send()` caller behaves sensibly on timeout: field saves, create, convert, delete, drag-move, the Add column dialog and the `#` picker search. The picker ignores stale responses by `seq`, so a superseded search never shows an error.

## Acceptance criteria

- [x] A field save that never settles ends with an error line after 30 s, the previous value restored, and the X and Escape working
- [x] The response body read is covered by the same timeout, not only the headers
- [x] A stale picker search that times out shows nothing; a current one shows "Search failed: …"
- [x] Verified in the browser with `fetch` stubbed to never settle

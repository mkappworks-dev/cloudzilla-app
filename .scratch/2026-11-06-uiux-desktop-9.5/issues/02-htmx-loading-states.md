# Loading states for htmx actions; toasts instead of alert()

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/htmx-loading-states` | `.worktrees/feat+htmx-loading-states` | `feat(ui): busy state on htmx requests and toasts for client errors` | 1, lane B | S | Interaction +1.0 |

## Problem

No htmx control has a loading or disabled state (zero `hx-indicator`/`hx-disabled-elt` in `internal/view`). Merge runs with no spinner, so double submits are possible. Five templates report failures with `window.alert()`.

## Scope

- One delegated listener (new `cmd/server/frontend/static/htmx-busy.js`, loaded via `assets.URL` in `layout.templ`) sets `aria-busy="true"` and `disabled` on the triggering button/form on `htmx:beforeRequest` and clears them on `htmx:afterRequest`. Mind the htmx 4 event shape (memory: htmx-redirect-refresh-skip-successful).
- A `.htmx-request` style in `tailwind/input.css`: reduced opacity plus a spinner on buttons; respects reduced motion.
- Replace `alert()` with the toast component in `fragments/repo_topics.templ`, `fragments/pull_chrome.templ`, `pages/gist_detail.templ`, `pages/gist_new.templ`, `pages/gist_edit.templ`.

## Files

- `cmd/server/frontend/static/htmx-busy.js (new)`
- `internal/view/layout/layout.templ`
- `tailwind/input.css`
- `internal/view/fragments/repo_topics.templ`
- `internal/view/fragments/pull_chrome.templ`
- `internal/view/pages/gist_*.templ`

## Acceptance criteria

- [x] Every htmx button is disabled and announced busy while its request runs
- [x] A failed request re-enables the control
- [x] No `alert(` left in `internal/view`
- [x] Merge cannot be double-submitted

## Verification

Live check on a throwaway server with network throttled; `grep -rn "alert(" internal/view` is empty.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (implement): done. Busy state keys on `button[aria-busy]`, not `.htmx-request`: htmx puts that class on the
form for a form submit, not on its button. `grep -rn "alert(" internal/view` still matches `alert(1)` XSS fixtures in
`_test.go` files; no template calls `alert(`.

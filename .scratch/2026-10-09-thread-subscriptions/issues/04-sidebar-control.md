# Subscribe control in the thread sidebars

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 01

## What

Per `mock.html` tab 1: a Notifications section with a Subscribe/Unsubscribe button, a "Muted" pill and a reason line, on `issue_detail.templ`, `pull_detail.templ` (replacing the repo-watch button at ~294-322) and `discussion_detail.templ`. One fragment under `internal/view/fragments/`, one view-model in `internal/view/viewmodels_*.go`, one handler pair for `PUT /api/repos/{owner}/{repo}/{issues|pulls|discussions}/{number}/subscription`, route in `internal/router/router.go`. The PUT re-renders the section via HTMX; no page reload or session-storage toast.

Hidden for anonymous viewers. The reason line texts are the ones in the mock; muted shows "You won't be notified unless someone @-mentions you."

## Acceptance criteria

- [ ] All three pages show the control in each of the mock's states; the button toggles without a reload.
- [ ] Subscribe on a muted or unsubscribed thread writes `subscribed/manual`; Unsubscribe writes `muted`, including when the user only watches the repo.
- [ ] The control never changes the repo watch.
- [ ] 401 for anonymous, 404 for a repo the caller can't read, 422 for a bad `state`.
- [ ] Render tests per page; `docs/api-reference.md` documents the endpoint.

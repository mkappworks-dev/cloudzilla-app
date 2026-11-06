# Same-page redirects reload, and dialogs scroll

Created: 2026-11-06
Category: bug
Status: ready-for-agent
Blocked by: 06

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/settings-redirect-dialog-scroll` | `.worktrees/fix+settings-redirect-dialog-scroll` | `fix(ui): reload after same-page saves and scroll tall dialogs` | 1, step 3 | S | Interaction +0.2, Edge +0.2 |

## Problem

Found during ticket 06's live check (#270).

1. `redirectAfterSave` (`internal/handler/handler.go:76`) answers an htmx request with `HX-Redirect`. When the target
   differs from the current URL only by its hash, the browser makes a hash-only navigation and does not reload. Creating
   a token on `/settings` (`access_token_handler.go:96`, target `/settings#tokens`) leaves the page unchanged: the new
   token and its one-time secret only show after a manual reload. The same shape exists at `totp_handler.go:67,97`
   (`/settings#security`) and `org_handler.go:339` (`/orgs/{org}/settings#repo-defaults`).
2. `components.Dialog` (`internal/view/components/dialog.templ:11`) sets `mt-[28vh] mb-auto` with no max height or
   overflow. The generate-token dialog's submit button sits below the fold in a 900px-tall window and the dialog
   cannot scroll. 52 dialogs use the component.

## Scope

- Make every `redirectAfterSave` to the current page actually reload, keeping the hash so the page lands on the right
  section and the flash cookie still shows (e.g. `HX-Refresh` or a full `location.assign` when only the hash differs;
  pick the htmx 4 mechanism and say why in the PR).
- `components.Dialog`: cap the height to the viewport and let the body scroll, keeping header and footer visible;
  reduce the top margin when the dialog is tall. Check a short dialog still sits where it does today.

## Files

- `internal/handler/handler.go` (+ its test)
- `internal/view/components/dialog.templ`
- `mockups/settings.html` and any mock with a tall dialog, if the dialog markup changes

## Acceptance criteria

- [ ] Creating a token on `/settings` shows the new token without a manual reload
- [ ] Enabling and disabling TOTP and saving org repo defaults show their result without a manual reload
- [ ] The generate-token dialog's submit button is reachable at 1440×900, light and dark
- [ ] Short dialogs (delete confirm, change password) look unchanged
- [ ] Handler test for the same-page redirect

## Verification

Throwaway server at 1440×900 in light and dark: create a token, toggle TOTP, save org repo defaults, open every tall
settings dialog.

## Comments

2026-11-06 (triage): ready-for-agent. Found in #270's live check; split out of 06.

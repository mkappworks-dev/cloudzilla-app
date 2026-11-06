# Same markup before and after an htmx swap

Created: 2026-11-06
Category: bug
Status: ready-for-agent
Blocked by: 02

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/htmx-fragment-parity` | `.worktrees/fix+htmx-fragment-parity` | `fix(ui): refresh fragments render the page markup` | 1, step 2 | S | Interaction +0.2, Edge +0.3 |

## Problem

After an action, handlers swap in older fragments that look different and drop safeguards: `fragments/ssh_keys.templ:22-29` Delete has **no confirm**; `fragments/tokens.templ` swaps to an old table; `fragments/admin_invitations.templ` and `fragments/refs.templ` restyle rows and drop `aria-label`s.

## Scope

- Extract the row markup each page renders into one component and use it from both the page and the fragment.
- Every destructive button keeps `hx-confirm` + `data-confirm-title` after a swap.
- Test: render page and fragment for the same data and compare the row HTML.

## Files

- `internal/view/fragments/ssh_keys.templ`
- `internal/view/fragments/tokens.templ`
- `internal/view/fragments/admin_invitations.templ`
- `internal/view/fragments/refs.templ`
- `internal/view/pages/settings.templ`
- `internal/view/pages/admin_settings.templ`
- `internal/view/pages/refs.templ`
- `internal/handler/ssh_key_handler.go`
- `internal/handler/access_token_handler.go`
- `internal/handler/admin_handler.go`

## Acceptance criteria

- [ ] SSH key delete asks for confirmation after a refresh
- [ ] Rows look identical before and after add/delete for keys, tokens, invitations, refs
- [ ] Page-vs-fragment test per list

## Verification

Add then delete an SSH key, a token, an invite and a branch on a throwaway server.

## Comments

2026-11-06 (triage): ready-for-agent.

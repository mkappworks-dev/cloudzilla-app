# Inline field validation and recovery

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/inline-validation-recovery` | `.worktrees/fix+inline-validation-recovery` | `fix(ui): inline field validation and recovery` | polish (wave 2 re-score) | M | Edge +0.4, A11y +0.2 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. Required-field errors are the browser's native "Please fill out this field." bubble: light in dark mode, only the first field, no inline text, nothing for `aria-describedby` (Claude and Codex, `issue-new-error`, `register-invalid`). Required fields are not marked in their labels.
2. Server errors sit in a banner away from the field: a duplicate repository name shows above the buttons and the name field has no invalid state (`repo-new-error`). A wrong password in the admin-mode dialog closes it and shows a banner (`admin-mode-identity-error`). A failed login clears the email field (`login-wrong-password`).
3. (Claude, unverified) Registration accepted `not-an-email` and a five-character password in the capture; check whether the server validates them or only the browser does.

## Scope

- Inline, themed field errors with `aria-invalid` and `aria-describedby`, focus on the first invalid field, required status in the label.
- Keep the identity dialog open with an inline error; keep the email on a failed login; attach the duplicate-name error to the name field.
- Share one mechanism with the keep-the-input work in ticket 11 (merged) and ticket 27.

## Files

- `internal/view/components/` form field components
- `new_repo.templ`, `issue_new.templ`, `register.templ`, `login.templ`, `components/identity_dialog.templ`
- matching mocks

## Acceptance criteria

- [x] No native validation bubble on the main create forms
- [x] Server errors appear next to their field and move focus
- [x] The identity dialog shows its error inline
- [x] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Build on ticket 11's keep-the-input mechanism; one inline error pattern for client and server errors.

2026-11-12 (implementation): item 3 checked. The server already enforced the 8-character password; it did not check the email format on `/register` (only the browser's `type=email` did), so `PageRegisterSubmit` now runs `validEmail`. The shared pattern is `static/field-validation.js` plus `components.FieldError` and `components.Invalid`. Not covered: the settings forms that use `ConfirmFields` outside the identity dialog, and the other create pages (discussion, milestone, release, wiki).

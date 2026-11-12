# Generate account, org, auth and admin mocks

Created: 2026-11-12
Category: enhancement
Status: wontfix
Blocked by: 01

| Branch                 | Worktree                         | PR title                                              | Effort |
| ---------------------- | -------------------------------- | ----------------------------------------------------- | ------ |
| `tech/mockgen-account` | `.worktrees/tech+mockgen-account` | `tech(mockups): generate account, org and admin mocks` | L      |

## Problem

See [spec.md](../spec.md). Follows the harness from 01.

## Scope

Register, generate and delete the hand mocks for: `profile`, `org_profile`, `organizations`, `new_organization`,
`org_settings`, `account_settings`, `my_repositories`, `my_pulls`, `my_issues`, `attention`, `notifications`, `stars`,
`new_repo`, `repo_import`, `repo_transfers`, auth pages (`register`, `forgot_password`, `reset_password`,
`verify_email`, `signup_complete`, `totp_verify`, `invite`), and admin (`admin_settings`, `admin_users`, `admin_user`,
`audit_log`, `sso_settings`, `admin_mode` in both states, `forbidden` with and without the superadmin hint). Add the
pages `COVERAGE.md` lists with no mock if they render from a plain view-model: `explore`, `setup`, `oauth_authorize`,
`device_login`. Form validation errors become `--error` variants. Regenerate `index.html` and `COVERAGE.md`.

## Files

- `cmd/mockgen/**`, `mockups/generated/**`, deleted hand mocks, `mockups/index.html`, `mockups/COVERAGE.md`

## Acceptance criteria

- [ ] Every listed page generated; its hand mock deleted
- [ ] Admin mode banner variant exists for a representative page
- [ ] Pages that cannot render from a plain view-model are listed in `COVERAGE.md` with the reason
- [ ] `tests/mockgen-diff` passes for each page
- [ ] Checked at 1440 in light and dark under `make mockups`
- [ ] CI `mockups` job green

## Verification

As in ticket 01.

## Comments
2026-11-12 (maintainer): wontfix. Mocks stay design-first and hand-written, so nothing is generated; see [spec.md](../spec.md). The unmerged branch and PR for this ticket are closed by the maintainer.

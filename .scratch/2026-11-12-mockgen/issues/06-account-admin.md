# Generate account, org, auth and admin mocks

Created: 2026-11-12
Category: enhancement
Status: done
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

- [x] Every listed page generated; its hand mock deleted
- [x] Admin mode banner variant exists for a representative page
- [x] Pages that cannot render from a plain view-model are listed in `COVERAGE.md` with the reason
- [x] `tests/mockgen-diff` passes for each page
- [x] Checked at 1440 in light and dark under `make mockups`
- [x] CI `mockups` job green

## Verification

As in ticket 01.

## Comments

2026-11-12 (agent): Every listed page renders from a plain view-model, so `COVERAGE.md` lists none as skipped. The CI
`mockups` job runs the same command as the local freshness check (`go run ./cmd/mockgen`, then `git diff --exit-code`),
which passes here. `setup` and `totp_verify` have no `tests/mockgen-diff` route: a seeded instance redirects both. Two
templates read the wall clock (`HeatmapLarge` on the profile, `compactRelative` on `my_repositories`), so the harness
rewrites their output onto the fixture clock in `cmd/mockgen/freeze_clock.go`; ticket 02's repo page needs the same for
its heatmap month labels.

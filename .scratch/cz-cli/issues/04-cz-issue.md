# cz issue commands

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 02, 03

Part of [spec](../spec.md). Blocked by 03 for the `origin`-remote default.

## What to build

- `cz issue list [-R owner/repo] [--state open|closed] [--label X]` → `GET /api/repos/:owner/:repo/issues/`
- `cz issue view <number>` → issue plus `…/comments`
- `cz issue create --title --body [--label] [--assignee]` → `POST …/issues/`, then label and assignee calls if given
- `cz issue comment <number> --body` → `POST …/issues/:number/comments`
- `cz issue close <number>` → `PATCH …/issues/:number`

Body from `--body`, `--body-file` or `$EDITOR` when run on a terminal with neither.

Requires `issues:write` for writes. Check the exact PATCH fields for closing in `internal/handler` before writing the client.

## Acceptance criteria

- [x] Each command works against a test server; `-R` overrides the repo from the `origin` remote.
- [x] `create` with a label or assignee that doesn't exist fails with the server's message and says whether the issue was already created.
- [x] A token without `issues:write` gets the scope message from 02.
- [x] Output is a table on a terminal, JSON when piped or with `--json`.

## Comments

- `issue list --label` dropped: the list endpoint takes no filter and its JSON carries no labels. `--state` is filtered client-side; the server returns at most 500 issues.
- `create` resolves `--label` names to IDs before creating (unknown label: nothing is created). Assignee/label-apply failures after creation report the issue number and URL.

# cz pr commands

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03

Part of [spec](../spec.md).

## What to build

- `cz pr list [-R owner/repo] [--state]` → `GET /api/repos/:owner/:repo/pulls/`
- `cz pr view <number>` → PR, reviews, line comments
- `cz pr create --base --head --title --body` → `POST …/pulls/`; `--head` defaults to the current branch, `--base` to the repo's default branch
- `cz pr merge <number> [--ff|--merge|--squash]` → `PATCH …/pulls/:number` with `state=merged` and `merge_strategy`
- `cz pr close <number>` → `PATCH …/pulls/:number` with `state=closed`
- `cz pr review <number> --approve | --request-changes | --comment --body` → `POST …/pulls/:number/reviews`

Scopes: `pulls:write` opens everything above except merging, which needs `repo:write`. A `403` on `merge` must say so rather than only naming the scope.

Out of scope: `pr checkout` and `pr diff`. The API reference lists no diff endpoint; add them once the server has one.

## Acceptance criteria

- [ ] Each command works against a test server, with the same `-R`, `--json` and body-input rules as 04.
- [ ] `merge` reports a conflict or a failed branch-protection check with the server's reason and a non-zero exit.
- [ ] `create` fails with a clear message when `--head` isn't pushed to the remote.
- [ ] A `pulls:write`-only token that runs `merge` is told it needs `repo:write`.

## Comments

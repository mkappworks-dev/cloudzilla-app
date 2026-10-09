# cz pr commands

Created: 2026-10-09
Category: enhancement
Status: done
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

- [x] Each command works against a test server, with the same `-R`, `--json` and body-input rules as 04.
- [x] `merge` reports a conflict or a failed branch-protection check with the server's reason and a non-zero exit.
- [x] `create` fails with a clear message when `--head` isn't pushed to the remote.
- [x] A `pulls:write`-only token that runs `merge` is told it needs `repo:write`.

## Comments

Built against the handlers, not just the API reference:

- The API takes `head_branch`/`base_branch`, not `base`/`head`; the flags stay `--base`/`--head`.
- `GET …/pulls/` has no state filter and returns every state, so `--state` (`open` by default, `closed`, `merged`, `all`) filters on the client.
- Every endpoint answers JSON without `HX-Request`; writes send `Content-Type: application/json` anyway.
- The server creates a PR for any head name, pushed or not, so `create` runs `git ls-remote --exit-code --heads <host>/<repo>.git <head>` with the token passed through the credential helper `repo clone` uses; exit 2 means not pushed.
- Merge failures are `422 merge blocked: <reason>` (changes requested, required checks, branch protection), `422` with the git error for a conflict or a non-open or draft PR, and `409` when a push moved the branch. A `pulls:write` token gets `403 insufficient_scope` naming `repo:write`; `cz` adds why.
- `close` reads the PR first: the server would rewrite a merged PR as closed.
- Review states are `approved`, `changes_requested`, `commented`; the server doesn't validate them and refuses reviewing your own PR with 422. `--request-changes` and `--comment` require a body.
- Added `--draft` (the API takes `is_draft`).

# Require-pull-request rule

Spec: `.scratch/require-pull-request-rule/spec.md`. Tickets: `.scratch/require-pull-request-rule/issues/01`–`03`.

A branch protection rule with `require_pull_request` refuses every direct update of a matching branch: a push (create, fast-forward, force, delete), a branch delete through the API, and a web file commit. Changes reach it only by merging a pull request. No admin bypass.

## Decisions

- A separate flag, default off. Implying it from `require_review_count > 0` would start refusing pushes on every existing rule that sets reviews.
- The server-side merge (`CheckMerge`) and applied suggestions on a PR's head branch are not direct updates and stay allowed.
- The refusal wraps `ErrPushRequiresPR` with the rule's pattern, so the pusher sees which rule. `checkPush` tests the flag first, so no history is walked.

## Tasks

1. **Flag (ticket 01).** Migration 108; `model.BranchProtection.RequirePullRequest`; every query in `branch_protection_store.go`; `Update` gains the parameter; `Create`/`Update` handlers read `require_pull_request`; checkbox in the Add rule dialog and a badge in `fragments/branch_protections.templ`; `ErrPushRequiresPR`. Test: service round-trip of the flag.
2. **Pushes (ticket 02).** Tests first in `branch_protection_service_test.go`: flagged rule refuses create, fast-forward, force and delete; unflagged and unmatched pass; the error names the rule and is not mapped to `ErrProtectionCheckFailed`. Then `checkPush` and `CheckPushCommand`. `DeleteBranch` maps the error to 422. Handler test: flagged `main` refuses `DELETE /branches`.
3. **Web commits (ticket 03).** Tests first in `internal/handler`: new file, edit, rename and delete on a flagged branch answer 422 and leave the branch where it was; an unmatched branch commits. Then `CheckWebCommit` and the three handlers.
4. **Docs and gates.** `api-reference.md`, `git-transport.md`, `code-browser.md`, `access-control.md` (branch protection rows); regenerate templ; `make lint`, `make test`, `make test-integration`; tick the tickets and set them `done`.

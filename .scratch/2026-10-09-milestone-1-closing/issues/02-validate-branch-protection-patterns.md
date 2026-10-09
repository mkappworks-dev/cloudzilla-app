# Reject invalid branch-protection patterns

Created: 2026-10-09
Category: bug
Status: ready-for-agent

## Problem

`MatchForBranch` skips any pattern `filepath.Match` rejects, but the create handler does not validate the pattern. `POST .../branches/protections/` with `pattern=[` returns 201 and the rule never applies, so a repo owner believes a branch is protected when it is not.

Found by reading the code during the coverage pass; reproduce first.

## Where

- `internal/store/branch_protection_store.go` `MatchForBranch` (~line 101)
- the create and update handlers in `internal/handler/branch_protection_handler.go`

## Acceptance criteria

- [ ] Create and update reject a pattern for which `filepath.Match(pattern, "")` returns `filepath.ErrBadPattern`, with 422 and a message naming the problem.
- [ ] Router tests for `[`, `a[`, `\` (trailing escape) and a valid glob such as `release/*`; refused requests store nothing.
- [ ] Decide what to do with rows already stored with a bad pattern (a migration to delete or flag them, or a startup log). Note the choice in the PR.
- [ ] `MatchForBranch` keeps skipping bad patterns defensively but logs once.

## Blocked by

Nothing.

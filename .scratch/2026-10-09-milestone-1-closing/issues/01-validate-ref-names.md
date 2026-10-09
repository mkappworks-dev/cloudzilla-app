# Validate branch and tag names

Created: 2026-10-09
Category: bug
Status: done

## Problem

`POST /api/repos/{o}/{r}/branches` and the tag equivalent write any name go-git will store. Names that native `git check-ref-format` rejects get 201 and a real ref: `a..b`, `x y`, `bad.lock`. A clone or fetch with the git CLI can then fail or misbehave. Path traversal is already refused (go-git rejects `../../x` and `a//b` with a 400).

Confirmed by running it against the router.

## Where

- `internal/handler/ref_handler.go` `CreateBranch` (~line 34) and `CreateTag` (~line 146)
- `internal/service/code_service_refs.go` (~lines 218 and 244)

## Acceptance criteria

- [x] A shared validator rejects names git rejects: `..`, a leading or trailing `/`, `//`, a component starting with `.` or ending in `.lock`, a trailing `.`, `@{`, a lone `@`, spaces, control characters, and any of `~ ^ : ? * [ \`.
- [x] `CreateBranch` and `CreateTag` answer 422 with a clear message for those names; valid names such as `feature/x`, `v1.2.3` and `fix_bug-2` still work.
- [x] Table test of the validator, plus a router test for each endpoint showing no ref was written on refusal.
- [x] Rename and any web "create branch" path use the same validator (grep for other callers of the create-ref code).

## Blocked by

Nothing.

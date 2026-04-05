# Handler Security Hardening Plan

**Branch:** `tech/m1-licensing-security-cleanup`
**Date:** 2026-04-05

## Problem

Security audit found missing authorization checks, unvalidated inputs, and no request body size limits across handlers.

## Fixes (6 commits, atomic)

### Commit 1: Add CanWrite authorization to state-mutating handlers

**Files:** `issue_handler.go`, `pull_handler.go`, `release_handler.go`

Add `CanWrite` check before any mutation:
```go
claims, ok := middleware.ClaimsFromContext(r.Context())
if !ok { writeError(w, 401, "unauthorized"); return }
repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
if err != nil { writeError(w, 404, "repo not found"); return }
if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
    writeError(w, 403, "forbidden"); return
}
```

Affected handlers:
- `UpdateIssue` — add CanWrite before SetState
- `UpdatePull` — add CanWrite before SetState/SetDraft/merge
- `UpdateRelease` — add CanWrite before Update
- `DeleteRelease` — add CanWrite before Delete

### Commit 2: Validate state enums

**Files:** `issue_handler.go`, `pull_handler.go`

After parsing state from form/JSON, reject invalid values:
- Issues: only `open`, `closed`
- PRs: only `open`, `closed`, `merged`

Return 400 with descriptive error for anything else.

### Commit 3: Fix ignored strconv errors

**Files:** `issue_handler.go`, `pull_handler.go`, `release_handler.go`, `pull_line_comment_handler.go`, `branch_protection_handler.go`, `repo_handler.go`

Replace all `thing, _ := strconv.Atoi/ParseInt(...)` with proper error checks returning 400.

### Commit 4: Add request body size limit middleware

**File:** `internal/middleware/body_limit.go`, `internal/router/router.go`

Create `MaxBodySize` middleware that wraps `r.Body` with `http.MaxBytesReader(w, r.Body, 1MB)`.
Apply globally in the API router group. Git transport routes already have their own handling.

### Commit 5: Fix open redirect in oauth_app_handler

**File:** `oauth_app_handler.go`

Validate `next` parameter is a relative path (starts with `/`, no `//` prefix, no scheme).

### Commit 6: Check ParseForm errors

**Files:** All handlers calling `r.ParseForm()` without error check (~13 locations)

Add `if err := r.ParseForm(); err != nil { writeError(w, 400, "invalid form"); return }` pattern.
Skip files that already check (some do, some don't — only fix the ones that don't).

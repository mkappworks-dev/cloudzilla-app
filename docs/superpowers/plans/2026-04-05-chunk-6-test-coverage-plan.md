# Milestone 1 — Chunk 6: Test Coverage Plan 🔄 IN PROGRESS

> **Scope:** Out of scope for `tech/m1-licensing-security-cleanup`. Implement on a separate branch (e.g., `tech/chunk-6-test-coverage`). Note: Chunk 4's permission model changes added tests for guard functions and `IsOwner`.

**Goal:** Document testing gaps, establish test infrastructure, and write critical-path tests prioritized by security impact.

---

## Current State

- **18 test files**, 772 lines total across 60k lines of code
- **All tests** are pure-function unit tests (guard checks, parsers, validators)
- **Several tests** reimplement logic inline rather than testing actual functions (zero regression value)
- **Zero** handler, middleware, store, SSH, or git transport tests
- **One** integration test (`issue_store_visibility_test.go`) uses `TEST_DATABASE_DSN`

---

## Audit of Existing Tests

| File | Lines | What's Tested | What's Missing |
|------|-------|---------------|----------------|
| `auto_merge_test.go` | 61 | `autoMergeGuard()` state validation | Actual merge scheduling, DB, concurrency |
| `dependency_service_test.go` | 117 | 4 parser functions (Go, npm, pip, cargo) | `ParseAndStore()` integration, malformed input |
| `gist_service_test.go` | 74 | `validateGistFiles()`, `generateGistID()` | CRUD with store, auth checks |
| `event_service_test.go` | 49 | JSON marshal (trivial), page clamp (reimplemented) | Actual event recording, feed retrieval |
| `topic_service_test.go` | 52 | `validateTopicName()`, too-many guard | SetTopics with store |
| `reaction_test.go` | 30 | `validEmoji()` allowlist | Toggle/list with store |
| `repo_archive_test.go` | 51 | `archiveGuard()`, `templateGuard()` | Archive/unarchive with store + git |
| `template_test.go` | 23 | `templateName()` string transform | Template listing |
| `index_service_test.go` | 23 | `isBinaryContent()` | Actual indexing flow |
| `issue_pinlock_test.go` | 27 | `pinLimitGuard()` | Pin/unpin/lock with store |
| `watch_service_test.go` | 27 | Inline validation (reimplemented, not real code) | WatchRepo/UnwatchRepo |
| `webhook_service_test.go` | 28 | `webhookBackoff()` | Dispatch, retry, HMAC signing |
| `saved_reply_service_test.go` | 28 | `savedReplyValidate()` | CRUD operations |
| `explore_service_test.go` | 26 | `periodToDuration()` | Explore query, ranking |
| `discussion_service_test.go` | 40 | Inline title validation (reimplemented) | Create/reply with store |
| `repo_soft_delete_test.go` | 19 | `deleteGuard()` | Soft delete + restore |
| `code_service_profile_readme_test.go` | 16 | `GetProfileReadme()` with missing repo | With existing repo |
| `issue_store_visibility_test.go` | 133 | `ListByRepo` visibility filtering (real PG) | Other store methods |

---

## Priority Matrix

### P0 — Security-Critical (must test first)

1. **Auth middleware** — JWT validation, PAT validation, OAuth resolution, cookie extraction, algorithm enforcement, `claimsFromMap` safety
2. **Setup middleware** — pre-setup lockdown, path allowlist
3. **SSH public key auth** — user key vs deploy key fallback, repo scoping, read-only enforcement
4. **Git HTTP auth** — `resolveGitUser()`, write/read access checks
5. **AccessTokenService.Validate** — SHA-256 hash matching, expiry, prefix check
6. **TOTP verification** — code check, backup code fallback
7. **Permission checks** — CanRead, CanWrite, CanManage with role combinations

### P1 — Data Integrity

1. **Store layer CRUD** — all 30+ stores essentially untested
2. **BranchProtectionService** — review count enforcement, status check matching, force push blocking
3. **Issue visibility filtering** — private issue scoping
4. **Webhook HMAC signing** — signature correctness

### P2 — Business Logic

1. **PR merge strategies** — ff, merge, squash
2. **Issue/PR state transitions** — open/close/reopen/merge
3. **Org membership** — add/remove/role transitions
4. **Fork creation** — repo + git directory duplication
5. **OAuth app flow** — code generation, exchange, token issuance
6. **Notification dispatch** — correct recipients, self-action suppression

### P3 — User-Facing

1. **Handler request/response** — JSON and HTML responses
2. **HTMX fragment rendering** — correct partial HTML
3. **Error response consistency** — status codes, message format

---

## Test Strategy

### Unit Tests (no DB)

Best for pure functions and middleware. The auth middleware already exposes `PATValidator` and `OAuthUserIDResolver` interfaces — fully mockable.

### Integration Tests (real DB)

Use the established `TEST_DATABASE_DSN` pattern. Create a shared helper package.

**Why not testcontainers?** No test container dependency in `go.mod`. The `TEST_DATABASE_DSN` pattern is established. Testcontainers is a larger infrastructure change for M2.

### HTTP Handler Tests

Use `net/http/httptest`. The `Handler` struct with `Services` pointer enables injection.

**The interface problem:** All stores are concrete structs. Two options:

- **Option A (M1):** Test handlers with real `Services` backed by test DB. Slower but validates full stack.
- **Option B (M2):** Extract store interfaces for mock-based unit tests.

**Recommendation:** Option A for M1 P0 tests. Middleware tests are fully unit-testable via existing interfaces.

---

## Proposed Test Files

### Infrastructure (build first)

| # | File | Purpose | Lines |
|---|------|---------|-------|
| 0 | `internal/testutil/testdb.go` | `OpenTestDB`, `SeedUser`, `SeedRepo`, `Cleanup` helpers | ~120 |

### P0 — Security-Critical

| # | File | Covers | Lines | DB? |
|---|------|--------|-------|-----|
| 1 | `internal/middleware/auth_test.go` | Auth, OptionalAuth, RequireSuperadmin, extractToken, claimsFromMap | ~300 | No |
| 2 | `internal/middleware/setup_test.go` | RequireSetup redirect, path allowlist | ~80 | No |
| 3 | `internal/service/access_token_service_test.go` | Generate (hash), Validate (prefix, expiry, hash match) | ~150 | Yes |
| 4 | `internal/service/repo_service_permission_test.go` | CanRead, CanWrite, CanManage with role combos | ~200 | Yes |
| 5 | `internal/handler/auth_handler_test.go` | Login (valid/invalid), Logout (cookie cleared) | ~150 | Yes |
| 6 | `internal/handler/git_http_test.go` | resolveGitUser, auth on info/refs | ~200 | Yes |
| 7 | `internal/service/totp_service_test.go` | Generate, Verify (valid/invalid), buildOTPAuthURL | ~120 | No |

### P1 — Data Integrity

| # | File | Covers | Lines | DB? |
|---|------|--------|-------|-----|
| 8 | `internal/store/user_store_test.go` | Create, GetByID, GetByUsername, GetByEmail | ~150 | Yes |
| 9 | `internal/store/repo_store_test.go` | Create, GetByOwnerName, GetPermission, Delete | ~200 | Yes |
| 10 | `internal/service/branch_protection_service_test.go` | CheckPush, CheckMerge | ~200 | Yes |
| 11 | `internal/service/webhook_service_test.go` (expand) | HMAC signing, PushPayload, dispatch retry | ~100 | No |

### P2 — Business Logic

| # | File | Covers | Lines | DB? |
|---|------|--------|-------|-----|
| 12 | `internal/service/issue_service_test.go` | Create (visibility), state transitions | ~150 | Yes |
| 13 | `internal/service/pull_service_test.go` | Create, merge strategies, state transitions | ~200 | Yes |
| 14 | `internal/service/org_service_test.go` | Create, add/remove members, roles | ~150 | Yes |
| 15 | `internal/service/oauth_app_service_test.go` | CreateApp, auth code flow, token exchange | ~180 | Yes |

### P3 — User-Facing

| # | File | Covers | Lines | DB? |
|---|------|--------|-------|-----|
| 16 | `internal/handler/setup_handler_test.go` | PageSetup redirect, PageSetupSubmit | ~100 | Yes |
| 17 | `internal/handler/issue_handler_test.go` | CreateIssue, ListIssues, UpdateIssue | ~200 | Yes |

---

## Auth Middleware Test Cases (Highest ROI)

The single most valuable test file in the entire project. All unit tests, no DB needed:

```
Valid JWT in cookie → 200, claims in context
Valid JWT in Authorization header → 200, claims in context
Expired JWT → 401
JWT signed with wrong algorithm (RS256) → 401
Valid PAT (czp_...) → 200, claims in context
Expired PAT → 401
Invalid PAT → 401
Valid OAuth token → 200, claims in context
No token at all → 401
RequireSuperadmin with superadmin claims → 200
RequireSuperadmin with regular claims → 403
OptionalAuth with no token → 200, no claims
OptionalAuth with valid token → 200, claims in context
claimsFromMap with missing "sub" → error (not panic)
claimsFromMap with missing "username" → error (not panic)
```

---

## Effort Estimates & ROI

**Highest safety per line of test code (implement first):**

1. `middleware/auth_test.go` (~300 lines) — protects entire auth boundary, fully unit-testable. **Best ROI.**
2. `middleware/setup_test.go` (~80 lines) — protects first-run lockdown. Fully unit-testable.
3. `service/totp_service_test.go` (~120 lines) — protects 2FA. Pure crypto, no DB.
4. `service/access_token_service_test.go` (~150 lines) — protects PAT auth.
5. `testutil/testdb.go` (~120 lines) — unblocks all integration tests.

**Recommended schedule:**

- **Week 1:** Items 0-3 + 7 (testdb, auth_test, setup_test, totp_test, access_token_test) — ~770 lines, all P0, mostly unit tests
- **Week 2:** Items 4-6 (permissions, auth handler, git HTTP) — ~550 lines, P0 integration tests
- **Week 3:** Items 8-11 (store tests, branch protection, webhook) — ~650 lines, P1
- **Future M2:** Items 12-17, store interfaces for mocking — ~980 lines, P2/P3

**Total new test code:** ~2,950 lines across 18 files (~4x current test volume). First 1,320 lines (weeks 1-2) cover all P0 security paths.

---

## Test Infrastructure Setup

### Create `internal/testutil/testdb.go`

```go
package testutil

// OpenTestDB opens a connection to the test database.
// Skips the test if TEST_DATABASE_DSN is not set.
func OpenTestDB(t *testing.T) *sql.DB

// SeedUser creates a test user with a unique suffix.
func SeedUser(t *testing.T, db *sql.DB, username string) *model.User

// SeedRepo creates a test repository owned by the given user.
func SeedRepo(t *testing.T, db *sql.DB, ownerID int64, name string) *model.Repository

// Cleanup removes test data from the specified tables.
func Cleanup(t *testing.T, db *sql.DB, tables ...string)
```

### Create `docker-compose.test.yml`

```yaml
services:
  test-db:
    image: postgres:16
    environment:
      POSTGRES_DB: cloudzilla_test
      POSTGRES_USER: cloudzilla
      POSTGRES_PASSWORD: test
    ports:
      - "5433:5432"
```

### Add to Makefile

```makefile
test-db:
    docker compose -f docker-compose.test.yml up -d
    @echo "TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable"

test: test-db
    TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable go test ./...
```

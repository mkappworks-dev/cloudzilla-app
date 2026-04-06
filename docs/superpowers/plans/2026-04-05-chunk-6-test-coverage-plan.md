# Milestone 1 — Chunk 6: Test Coverage Plan 🔄 EXTENDED

> **Scope:** Implement on `tech/chunk-6-test-coverage`. Phase 1 (original 18 files) complete. Phase 2 (extended audit) in progress.
>
> **Go test convention:** Test files live co-located with source (`foo.go` + `foo_test.go` in the same directory). This is idiomatic Go — a separate `test/` root folder is a non-Go pattern. Both `package foo` (white-box) and `package foo_test` (black-box) are used; both live in the same directory.

**Goal:** Full test coverage of all critical handlers, services, and stores across the codebase.

---

## Phase 1 — Original Chunk 6 ✅ COMPLETE

All 18 planned test files delivered (137 test functions):

| File | Tests |
|------|-------|
| `testutil/testdb.go` | infrastructure |
| `middleware/auth_test.go` | 17 |
| `middleware/setup_test.go` | 5 |
| `service/totp_service_test.go` | 12 |
| `service/access_token_service_test.go` | 8 |
| `service/repo_service_permission_test.go` | 13 |
| `handler/auth_handler_test.go` | 6 |
| `handler/git_http_test.go` | 7 |
| `store/user_store_test.go` | 8 |
| `store/repo_store_test.go` | 7 |
| `service/branch_protection_service_test.go` | 11 |
| `service/webhook_service_test.go` | 8 |
| `service/issue_service_test.go` | 6 |
| `service/pull_service_test.go` | 6 |
| `service/org_service_test.go` | 6 |
| `service/oauth_app_service_test.go` | 8 |
| `handler/setup_handler_test.go` | 2 |
| `handler/issue_handler_test.go` | 6 |

---

## Phase 2 — Extended Coverage (Deep Audit)

**Audit findings:** 530+ untested public methods across 53 handler files, 32 service files, and 36 store files.

---

## Extended Priority Matrix

### P0 — Security-Critical (implement first)

| # | File | Covers | DB? |
|---|------|--------|-----|
| 19 | `service/user_service_test.go` | Create (password hash), Authenticate (timing-safe), CreateSuperadmin | Yes |
| 20 | `service/notification_service_test.go` | Self-action suppression, fan-out to watchers, MarkRead, CountUnread | Yes |

### P1 — Data Integrity

| # | File | Covers | DB? |
|---|------|--------|-----|
| 21 | `store/pull_store_test.go` | Create, GetByNumber, UpdateState, SetDraft, ListOpen | Yes |
| 22 | `store/org_store_test.go` | Create, AddMember, GetMemberRole, RemoveMember, IsMember | Yes |
| 23 | `service/comment_service_test.go` | CreateForIssue, GetByID, Update, Delete, ListByIssue | Yes |
| 24 | `service/label_service_test.go` | Create, ListByRepo, AddToIssue, RemoveFromIssue | Yes |
| 25 | `service/star_service_test.go` | Star, Unstar, IsStarred, GetStarCount | Yes |
| 26 | `store/comment_store_test.go` | CreateIssueComment, GetByID, Update, Delete, ListByIssue | Yes |

### P2 — Business Logic

| # | File | Covers | DB? |
|---|------|--------|-----|
| 27 | `handler/pull_handler_test.go` | CreatePull (no-auth 401, valid 201), UpdatePull state, ListPulls | Yes |
| 28 | `handler/notification_handler_test.go` | MarkRead, MarkAllRead, GetUnreadCount | Yes |
| 29 | `handler/comment_handler_test.go` | CreateIssueComment (no-auth 401, valid 201), DeleteComment | Yes |
| 30 | `handler/repo_handler_test.go` | CreateRepo, GetRepo, ListRepos, AddCollaborator | Yes |
| 31 | `service/gist_service_test.go` (expand) | Create, Get, Update, Delete with store | Yes |

### P3 — Coverage Breadth

| # | File | Covers | DB? |
|---|------|--------|-----|
| 32 | `store/label_store_test.go` | Create, ListByRepo, AddToIssue, RemoveFromIssue | Yes |
| 33 | `store/notification_store_test.go` | Create, ListByUser, MarkRead, CountUnread | Yes |
| 34 | `service/deploy_key_service_test.go` | Add, List, Delete, AuthenticatePublicKey | Yes |
| 35 | `service/release_service_test.go` | Create, ListByRepo, GetByTag, Update, Delete | Yes |
| 36 | `service/milestone_service_test.go` | Create, ListByRepo, Close, Reopen, SetIssue | Yes |
| 37 | `handler/admin_handler_test.go` | RequireSuperadmin gate, UpdateSiteSetting, CreateInvitation | Yes |

### Out of Scope (M2)

- `sso_service.go` — requires live LDAP/SAML test infra (testcontainers)
- `email_service.go` — requires SMTP mock server
- `code_service_merge.go` — requires real git repo fixtures on disk; complex to set up
- SSH server auth — requires live SSH TCP listener
- `oauth_handler.go` (Google OAuth) — requires Google OAuth mock

---

## Implementation Schedule

- **Wave 1 (P0):** Items 19-20 — user service password safety, notification self-suppression
- **Wave 2 (P1):** Items 21-26 — store CRUD and service logic tests
- **Wave 3 (P2):** Items 27-31 — handler integration tests
- **Wave 4 (P3):** Items 32-37 — breadth coverage

---

## Audit Summary (2026-04-06)

| Layer | Total Files | Have Tests | Coverage |
|-------|-------------|-----------|---------|
| Handlers | 57 | 4 → target 14 | 7% → 25% |
| Services | 47 | 16 → target 26 | 34% → 55% |
| Stores | 39 | 3 → target 9 | 8% → 23% |

**Security-critical untested methods:** `Authenticate` (password timing), `CreateSuperadmin` (privilege escalation), `NotifyX` (self-action suppression), `AuthenticateLDAP`, `HandleSAMLCallback`, `SAMLAuthnRequestURL`, `IsAssertionUsed`, OAuth state validation, deploy key auth, webhook HMAC delivery.

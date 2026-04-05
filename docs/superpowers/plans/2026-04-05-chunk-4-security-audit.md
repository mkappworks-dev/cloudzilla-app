# Milestone 1 — Chunk 4: Security Audit ✅ COMPLETE (P1/P2)

> **Status:** All Priority 1 (critical) and Priority 2 (high) tasks done on `tech/m1-licensing-security-cleanup`. Priority 3 items deferred to M2. Additional authorization model upgrade (CanManage/IsOwner) completed beyond original plan scope.

**Goal:** Identify and fix security vulnerabilities across HTMX, git transport, SQL, auth, SSO, and input validation layers.

---

## Findings Summary

| Severity | Finding | File | Line(s) |
|----------|---------|------|---------|
| CRITICAL | CORS allows all origins with credentials in production | `middleware/cors.go` | 11-13 |
| HIGH | No CSRF protection on any state-changing endpoint | `router/router.go` | all routes |
| HIGH | Default JWT secret is "change-me" with no startup check | `config/config.go` | 72 |
| MEDIUM | Branch protection enforced AFTER receive-pack completes | `git_http.go`, `ssh/server.go` | 287-301, 238-253 |
| MEDIUM | No Secure flag on cookies | all handler files | various |
| MEDIUM | No repo name input validation (path traversal risk) | `repo_service.go` | 30, 48 |
| MEDIUM | SAML XML signature wrapping not fully mitigated | `sso_service.go` | 624-697 |
| MEDIUM | SAML audience validation conditional on config | `sso_service.go` | 506-518 |
| MEDIUM | Webhook SSRF — no URL validation | `webhook_service.go` | 97 |
| MEDIUM | claimsFromMap panics on missing JWT claims | `middleware/auth.go` | 148-149 |
| MEDIUM | No JWT revocation mechanism | `user_service.go` | 158-167 |
| LOW | LIKE wildcard injection in search | `search_store.go` | 79 |
| LOW | OAuth-resolved users have empty Username | `middleware/auth.go` | 48-55 |
| LOW | No path traversal validation on git URL params | `git_http.go` | 46-47, 89 |

---

## Detailed Findings

### 1. HTMX Security

**FINDING 1.1 (CRITICAL) — CORS allows all origins with credentials in production**

File: `internal/middleware/cors.go` lines 11-13
```go
if !devMode {
    origins = []string{"*"}
}
```
In production, CORS allows ANY origin with `AllowCredentials: true`. The `go-chi/cors` library emits the request's `Origin` header as the allowed origin, enabling cross-origin credential attacks.

**Fix:** Use explicit origin allowlist. Add `cors.allowed_origins` config field.

**FINDING 1.2 (HIGH) — No CSRF protection**

Zero CSRF middleware exists. All HTMX `hx-post`/`hx-patch`/`hx-delete` rely solely on JWT cookies. `SameSite=Lax` mitigates GET-based CSRF but POST from attacker sites is possible on older browsers.

**Fix:** Add CSRF token middleware. For HTMX, inject token via `hx-headers='{"X-CSRF-Token": "..."}' ` on `<body>`.

**FINDING 1.3 (SAFE) — HX-Redirect values from trusted data**

Redirect headers built from DB-validated owner/repoName. No open redirect found.

**FINDING 1.4 (SAFE) — Content-Type headers correct**

HTML responses: `text/html; charset=utf-8`. JSON: `application/json`.

---

### 2. Git Transport Security

**FINDING 2.1 (MEDIUM) — No path traversal validation on owner/repo params**

File: `internal/handler/git_http.go` lines 46-47, 89

Owner and repoName from chi URL params used in `filepath.Join()`. While DB lookup gates access (repo must exist), no explicit rejection of `..`, `/`, `\0`.

File: `internal/ssh/server.go` lines 159-168

SSH splits by `/` requiring exactly 2 parts (prevents deep nesting) but `..` not explicitly rejected.

**Fix:** Add regex validation: `^[a-zA-Z0-9._-]+$` for both owner and repo name before any filesystem operation.

**FINDING 2.2 (SAFE) — Auth checks correctly implemented**

Git HTTP properly checks CanRead/CanWrite. SSH handles user-key and deploy-key paths, enforces deploy-key repo binding and read-only.

**FINDING 2.3 (MEDIUM) — Branch protection enforced after pack written**

File: `git_http.go` lines 287-301, `ssh/server.go` lines 238-253

`sess.ReceivePack()` writes pack data BEFORE protection check. HTTP returns 403 but refs are already updated on disk.

**Fix:** Move protection checks to pre-receive hook or validate before calling ReceivePack.

---

### 3. SQL Injection

**FINDING 3.1 (SAFE) — All queries use parameterized placeholders**

Every dynamic SQL query uses `$N` positional parameters. `IN (...)` clauses build `$1, $2, ...` placeholder lists from typed slices. No string concatenation of user input into SQL found.

**FINDING 3.2 (LOW) — LIKE wildcards in search**

File: `search_store.go` line 79

User search input can contain `%` and `_` LIKE wildcards. Low severity — only affects search result ordering, no data integrity risk.

**Fix:** Escape `%` and `_` in user search input before passing to LIKE.

---

### 4. Authentication & Authorization

**FINDING 4.1 (SAFE) — JWT algorithm confusion mitigated**

Auth middleware explicitly checks `t.Method.(*jwt.SigningMethodHMAC)` and rejects non-HMAC algorithms.

**FINDING 4.2 (HIGH) — Default JWT secret is "change-me"**

File: `config/config.go` line 72

No startup warning when using the default. Enables token forgery on misconfigured instances.

**Fix:** Add startup check that panics/warns if `jwt_secret == "change-me"`.

**FINDING 4.3 (MEDIUM) — No Secure flag on cookies**

Zero occurrences of `Secure: true` across all handler files. Cookies transmitted over plain HTTP.

**Fix:** Add `Secure` flag configurable via `auth.cookie_secure` setting (default true).

**FINDING 4.4 (MEDIUM) — No JWT revocation**

24h default expiry, no refresh flow, no revocation blocklist. Compromised JWTs cannot be invalidated.

**Fix:** Implement JWT blocklist or switch to server-side sessions. Lower priority for M1.

**FINDING 4.5 (MEDIUM) — claimsFromMap panics on missing claims**

File: `middleware/auth.go` lines 148-149

```go
UserID:   int64(m["sub"].(float64)),
Username: m["username"].(string),
```

Bare type assertions without ok-check. Panics if claims are missing from a valid JWT.

**Fix:** Use comma-ok assertions with fallback to error.

**FINDING 4.6 (LOW) — OAuth-resolved users have empty Username**

File: `middleware/auth.go` lines 48-55

When OAuth resolver succeeds, only `UserID` is set. `Username` and `IsSuperadmin` are zero-valued.

---

### 5. SSO/SAML Security

**FINDING 5.1 (SAFE) — Signature verification enforced**

IdP certificate required. `verifySAMLSignature()` validates XML digital signature with RSA-SHA256.

**FINDING 5.2 (SAFE) — Assertion replay protection implemented**

Assertion ID checked against `sso_assertions` table. Replayed assertions rejected.

**FINDING 5.3 (MEDIUM) — XML signature wrapping not fully mitigated**

File: `sso_service.go` lines 624-697

Uses raw byte manipulation instead of full XML Exclusive Canonicalization (EXC-C14N). Theoretically vulnerable to signature wrapping attacks.

**Fix:** Consider adopting `crewjam/saml` library for proper canonicalization.

**FINDING 5.4 (SAFE) — LDAP injection mitigated**

Username escaped via `escapeLDAPDN()` with RFC 4514 special character handling.

**FINDING 5.5 (MEDIUM) — Audience validation conditional**

File: `sso_service.go` lines 506-518

If admin doesn't configure `entity_id`, audience validation is completely skipped.

**Fix:** Make `entity_id` and `acs_url` required when SAML is enabled.

---

### 6. Null Pointer / Nil Dereference

**FINDING 6.1 (MEDIUM) — claimsFromMap panics** (same as 4.5)

**FINDING 6.2 (SAFE) — Deploy key casting properly guarded**

SSH server's deploy key nil checks are correct.

**FINDING 6.3 (SAFE) — Error handling generally good**

Most store/service methods check `err != nil` before using results. No systemic pattern of unchecked errors.

---

### 7. Input Validation

**FINDING 7.1 (MEDIUM) — No repository name validation**

File: `repo_service.go` line 30

No regex/allowlist for repo names. `../../etc` passed to `filepath.Join` line 48.

**Fix:** Validate repo and owner names: `^[a-zA-Z0-9._-]+$`, max 100 chars.

**FINDING 7.2 (SAFE) — Markdown XSS mitigated**

Goldmark link sanitizer rewrites `javascript:`, `vbscript:`, `data:` schemes. Raw HTML disabled. Mermaid content escaped with `template.HTMLEscape`.

**FINDING 7.3 (SAFE) — renderMentionsHTML safe**

Regex limits usernames to `[A-Za-z0-9_-]+`. Applied after markdown rendering, output via `templ.Raw()`.

**FINDING 7.4 (MEDIUM) — templ.Raw trust boundary**

`@templ.Raw(data.BodyHTML)` used extensively. Security depends on markdown renderer config. Currently safe, but any change to enable `WithUnsafe()` would introduce XSS.

**Fix:** Add a comment documenting this trust boundary. Consider a wrapper function that double-checks output.

**FINDING 7.5 (MEDIUM) — Webhook SSRF**

File: `webhook_service.go` line 97

User-provided webhook URLs can target internal services (`localhost`, `169.254.169.254`).

**Fix:** Block private/internal IP ranges in webhook URL validation.

---

## Implementation Tasks

### Priority 1 — Must Fix Before Launch ✅ ALL DONE

- [x] **Task 1:** Fix CORS — add `cors.allowed_origins` config, use explicit allowlist in production (`middleware/cors.go`) — commit `db6d889`
- [x] **Task 2:** Add startup check — panic/warn if `jwt_secret == "change-me"` — commit `726b236`
- [x] **Task 3:** Add CSRF middleware — double-submit cookie pattern, `X-CSRF-Token` header via HTMX `hx-headers` — commit `db6d889`

### Priority 2 — Should Fix ✅ ALL DONE

- [x] **Task 4:** Add input validation for repo/owner/branch names — `^[a-zA-Z0-9._-]+$` — commit `68465d9`
- [x] **Task 5:** Add `Secure` cookie flag — configurable via `auth.cookie_secure` — commit `726b236`
- [x] **Task 6:** Fix branch protection timing — rollback on violation before ReceivePack completes — commit `68465d9`
- [x] **Task 7:** Fix claimsFromMap — comma-ok type assertions — commit `726b236`
- [x] **Task 8:** Add webhook SSRF protection — block private IP ranges — commit `68465d9`

### Priority 3 — Should Improve (deferred to M2)

- [ ] **Task 9:** Make SAML `entity_id` and `acs_url` required when SAML enabled
- [ ] **Task 10:** Escape LIKE wildcards in search input
- [ ] **Task 11:** Document templ.Raw trust boundary
- [ ] **Task 12:** Consider JWT revocation or server-side sessions (larger effort, deferred to M2)

### Additional Work (beyond original plan)

- [x] **Task 13:** Add `MaxBodySize` middleware — 1 MB limit on all API routes — commit `3468224`
- [x] **Task 14:** Validate all URL path parameter parsing (strconv errors) — commit `3841431`
- [x] **Task 15:** Check ParseForm errors in all handlers — commit `b4c9b73`
- [x] **Task 16:** Fix open redirect in OAuth authorize flow — commit `cf2f2d5`
- [x] **Task 17:** Add CanWrite authorization to 18 unprotected state-mutating handlers — commits `a3cb53e`, `4026753`
- [x] **Task 18:** Upgrade admin role — `CanManage` includes admin collaborators, new `IsOwner` for destructive ops — commit `c95a000`
- [x] **Task 19:** Align handlers and views with new permission model (webhooks, collaborators, settings) — commit `9a41160`
- [x] **Task 20:** Comprehensive access-control documentation — commit `a2df86c`, `2f75a20`
- [x] **Task 21:** Remove SQLite support, standardize on PostgreSQL — commit `6ad536a`
- [x] **Task 22:** Remove sqlc, standardize all stores on direct sqlx queries — commit `e86fc13`

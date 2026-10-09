# Device Approval Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The browser half of `cz auth login`: a signed-in user enters the code shown in their terminal, sees what is being authorized, and approves with the same confirmation factors as any token-creating action, or denies.

**Architecture:** Three session-only pages (`/login/device` entry, `/login/device/confirm`, result) and two POSTs. The typed code is kept in a short-lived HttpOnly cookie, never in a URL, so the provider re-auth round-trip (Google/SAML) can return to the confirm page. Approval is `Reauth.Confirm` then `DeviceGrantService.Approve`; scopes can only be narrowed. Audit events, an email notice and a per-user entry limiter round it out. Stores → services → handlers, as in `CLAUDE.md`.

**Tech Stack:** Go, chi, Templ, HTMX/Alpine (existing layout), PostgreSQL.

**Spec:** `.scratch/2026-10-09-cz-cli/issues/07b-device-approval-page.md` and the Design section of `07-device-code-login.md` (both on branch `feat/cz-device-login`; PR open). This branch is stacked on `feat/cz-device-grant-api` (ticket 07a, PR open): `DeviceGrantService` and the two JSON endpoints already exist here.

## Global Constraints

- The user code is never carried in a URL: no `?user_code=`, no link that pre-fills it. `GET /login/device?user_code=…` ignores the parameter.
- Sessions only: the pages are closed to personal access tokens and OAuth tokens (`internal/middleware/scope.go` allows only listed routes; do not add these paths there). Do not add the routes to `internal/middleware/setup.go`.
- Code entry: 50 per hour per user (in-process, like the other auth limits). An unknown, expired or used code gets one generic message.
- Approving needs `Reauth.Confirm` (password plus a TOTP code with 2FA; LDAP directory password; Google/SAML "Confirm with…"; emailed code). Failed confirmations share the existing per-user throttle (5 in 15 minutes, `429`). Denying needs no confirmation.
- The scope check runs before the confirmation so a typo does not spend a failed attempt. The user may untick scopes, never add; at least one must remain; scopes outside the request are ignored; `repo:admin` is never grantable.
- The confirm and result pages send `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'` and `Cache-Control: no-store`.
- Device name is untrusted: show it escaped and labelled "unverified".
- Audit events `user.device.approve`, `user.device.deny`, `user.token.create` (the last for the token minted at the first successful poll, source "device login"). Approval mails the account a security notice (device name, IP, scopes).
- Layout of the confirm page, top to bottom: header "Authorize cz" with the signed-in username; warning banner "Only continue if you just ran `cz auth login` and this code matches your terminal."; details table (code, device name tagged "unverified", requester IP and time); "Access this token will have" with each scope a bordered, pre-ticked row and the hint "Untick to give less. You can't add access here."; "Confirm it's you" with only the account's factors; one primary **Authorize cz** button, a **Deny** button, and the line "You can revoke this token any time under Settings, Tokens."
- Comments only for a non-obvious why. Templ: edit `.templ`, run `make generate-templ`, commit the generated `_templ.go`; never run `templ fmt` on repo files.
- Integration tests need `TEST_DATABASE_DSN` (a migrated standalone Postgres).

## File Structure

- Modify `internal/model/audit_log.go` (three action constants)
- Modify `internal/service/device_grant_service.go` (+ test): `DeviceToken` gains `UserID`, `TokenName`; `WithEmail`, `NotifyApproved`
- Modify `internal/service/services.go`: wire email
- Modify `internal/middleware/rate_limit.go` (+ test): `UserLimiter`
- Modify `internal/handler/device_auth_handler.go`, `internal/router/device_auth_test.go`: audit `user.token.create` on the first successful poll
- Create `internal/view/viewmodels_device.go`, `internal/view/pages/device_login.templ` (+ generated, + `device_login_test.go`)
- Create `internal/handler/page_device_handler.go`
- Modify `internal/router/router.go`
- Create `internal/router/device_approval_test.go`
- Modify `docs/access-control.md`, `docs/api-reference.md`

---

### Task 1: Backend support (audit, notice, entry limiter)

**Files:**
- Modify: `internal/model/audit_log.go`, `internal/service/device_grant_service.go`, `internal/service/device_grant_service_test.go`, `internal/service/services.go`, `internal/middleware/rate_limit.go`, `internal/middleware/rate_limit_test.go`, `internal/handler/device_auth_handler.go`, `internal/router/device_auth_test.go`

**Interfaces:**
- Produces (`model`): `AuditActionDeviceApprove = "user.device.approve"`, `AuditActionDeviceDeny = "user.device.deny"`, `AuditActionTokenCreate = "user.token.create"`.
- Produces (`service`): `DeviceToken` gains `UserID int64` and `TokenName string`; `func (s *DeviceGrantService) WithEmail(e *EmailService) *DeviceGrantService`; `func (s *DeviceGrantService) NotifyApproved(userID int64, deviceName, ip string, scopes []string)` (fire-and-forget; no-op without email).
- Produces (`middleware`): `type UserLimiter`, `func NewUserLimiter(limit int, window time.Duration) *UserLimiter`, `func (l *UserLimiter) Middleware(onLimited http.HandlerFunc) func(http.Handler) http.Handler` — counts per signed-in user (`ClaimsFromContext`), sets `Retry-After`, calls `onLimited` over the limit; a request without claims passes through (auth middleware runs first).

- [ ] **Step 1: Failing tests**

`internal/middleware/rate_limit_test.go` (same package style as the existing tests there; reuse its fake-clock approach — read the file first):

```go
func TestUserLimiter_CountsPerUser(t *testing.T) {
	now := time.Now()
	l := newUserLimiter(2, time.Hour, func() time.Time { return now })
	limited := 0
	h := l.Middleware(func(w http.ResponseWriter, r *http.Request) { limited++; w.WriteHeader(http.StatusTooManyRequests) })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	call := func(userID int64) int {
		req := httptest.NewRequest("POST", "/x", nil)
		req = req.WithContext(context.WithValue(req.Context(), claimsKey, Claims{UserID: userID}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests && rec.Header().Get("Retry-After") == "" {
			t.Error("429 without Retry-After")
		}
		return rec.Code
	}
	if call(1) != 200 || call(1) != 200 || call(1) != 429 {
		t.Fatal("user 1 should get two requests, then 429")
	}
	if call(2) != 200 {
		t.Error("user 2 has a budget of its own")
	}
	now = now.Add(time.Hour + time.Second)
	if call(1) != 200 {
		t.Error("the window should reset after an hour")
	}
	if limited != 1 {
		t.Errorf("onLimited ran %d times; want 1", limited)
	}
}
```

The test is in-package, so it sets claims with the unexported `claimsKey`; do not add an exported helper just for the test.

`internal/service/device_grant_service_test.go`: assert the poll result carries the owner and the token name (extend the existing lifecycle test: `tok.UserID == uid`, `tok.TokenName == wantName`), and add:

```go
func TestDeviceGrantService_NotifyApprovedWithoutEmailIsANoop(t *testing.T) {
	svc, _, uid, _ := newDeviceSvc(t)
	svc.NotifyApproved(uid, "laptop", "203.0.113.7", []string{"repo:read"}) // must not panic
}
```

`internal/router/device_auth_test.go`: at the end of `TestDeviceLogin_EndToEnd`, after the successful token response, add `takeAudit(t, db, model.AuditActionTokenCreate, uid)` (helper from `sensitive_actions_test.go`; import `internal/model`).

Run: `go test ./internal/middleware ./internal/service ./internal/router -run 'UserLimiter|DeviceGrant|DeviceLogin' -v` — Expected: FAIL (undefined symbols / missing audit row).

- [ ] **Step 2: Audit constants** — in `internal/model/audit_log.go`, next to the other user actions:

```go
	AuditActionDeviceApprove = "user.device.approve"
	AuditActionDeviceDeny    = "user.device.deny"
	AuditActionTokenCreate   = "user.token.create"
```

If `audit_log.go` or another file keeps a list of known actions (for the admin audit page filter, labels or tests), add the three there too; find with `grep -rn 'AuditActionPasswordChange' internal`.

- [ ] **Step 3: `UserLimiter`** — in `internal/middleware/rate_limit.go`:

```go
// UserLimiter allows each signed-in user limit events per window. Counts live in
// this process, so each instance of a multi-instance deployment counts alone.
type UserLimiter struct {
	limit   int
	counter *fixedWindow
}

func NewUserLimiter(limit int, window time.Duration) *UserLimiter {
	return newUserLimiter(limit, window, time.Now)
}

func newUserLimiter(limit int, window time.Duration, now func() time.Time) *UserLimiter {
	return &UserLimiter{limit: limit, counter: newFixedWindow(window, now)}
}

// Middleware must run after the auth middleware; a request without claims passes.
func (l *UserLimiter) Middleware(onLimited http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if claims, ok := ClaimsFromContext(r.Context()); ok {
				if _, reset, allowed := l.counter.take("user:"+strconv.FormatInt(claims.UserID, 10), l.limit); !allowed {
					setRetryAfter(w, reset.Sub(l.counter.now()))
					onLimited(w, r)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Service** — in `internal/service/device_grant_service.go`: add fields/method

```go
type DeviceGrantService struct {
	grants *store.DeviceGrantStore
	tokens *store.AccessTokenStore
	users  *store.UserStore
	email  *EmailService
	now    func() time.Time
}
```

Change `NewDeviceGrantService(grants, tokens)` to also take `users *store.UserStore` (update `services.go` and every call in tests: `service.NewDeviceGrantService(stores.DeviceGrant, stores.AccessToken, stores.User)`), add

```go
func (s *DeviceGrantService) WithEmail(e *EmailService) *DeviceGrantService { s.email = e; return s }

// NotifyApproved mails userID that a device login was approved on their account.
func (s *DeviceGrantService) NotifyApproved(userID int64, deviceName, ip string, scopes []string) {
	if s.email == nil {
		return
	}
	concurrency.Go("device_login.notice", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := s.users.GetByID(ctx, userID)
		if err != nil {
			slog.Error("device login notice: load user", "user_id", userID, "error", err)
			return
		}
		if deviceName == "" {
			deviceName = defaultDeviceName
		}
		body := fmt.Sprintf("<p>A login from the device <strong>%s</strong> (IP %s) was approved on the Cloudzilla account <strong>@%s</strong>. It can use: <code>%s</code>.</p>"+
			"<p>If that wasn't you, revoke the token under Account settings → Access tokens and change your password.</p>",
			html.EscapeString(deviceName), html.EscapeString(ip), html.EscapeString(u.Username), html.EscapeString(strings.Join(scopes, " ")))
		if err := s.email.SendSecurityNotice(u, "A new device was authorized to use your account", body); err != nil {
			slog.Error("device login notice: send", "user_id", userID, "error", err)
		}
	})
}
```

(imports: `html`, and whichever package provides `concurrency.Go` in `reauth_service.go` — copy its import.) Extend `DeviceToken` with `UserID int64` and `TokenName string`, and in `Poll` set them inside the `Redeem` callback closure from `g.UserID.Int64` and `t.Name` (declare `var userID int64; var name string` beside `raw`). In `services.go` wire `NewDeviceGrantService(stores.DeviceGrant, stores.AccessToken, stores.User).WithEmail(emailSvc)` — use the same email service variable `NewReauthService(...).WithEmailCodes(...)` receives; read `services.go` to find it.

- [ ] **Step 5: Audit the minted token** — in `DeviceToken` (`internal/handler/device_auth_handler.go`) in the success branch, before `writeJSON`:

```go
		if u, err := h.Services.User.GetByID(r.Context(), tok.UserID); err != nil {
			slog.Error("device token: load user for audit", "user_id", tok.UserID, "error", err)
		} else {
			h.Services.AuditLog.Record(r.Context(), r, u.ID, u.Username, model.AuditActionTokenCreate, model.AuditTargetUser, u.ID, u.Username,
				map[string]any{"source": "device login", "token": tok.TokenName})
		}
```

(import `internal/model`.) Check `AuditLog.Record`'s signature in `internal/service/audit_log_service.go` and match it exactly.

- [ ] **Step 6: Run, then commit**

Run: `go test ./internal/middleware ./internal/service ./internal/router ./internal/handler -run 'UserLimiter|DeviceGrant|DeviceLogin|AccessToken' -race -v` (confirm no SKIP), `go build ./...`, `gofmt -l internal`, `go vet ./internal/...`. Expected: PASS.

```bash
git add internal/model internal/service internal/middleware internal/handler internal/router
git commit -m "feat(auth): audit device-login tokens, approval notice and per-user entry limiter"
```

---

### Task 2: View models and pages

**Files:**
- Create: `internal/view/viewmodels_device.go`, `internal/view/pages/device_login.templ`, `internal/view/pages/device_login_test.go` (+ generated `device_login_templ.go`)

**Interfaces:**
- Consumes: `view.BasePage`, `components.ConfirmFactors`, `components.ConfirmFields`, `components.Button*`, `layout.Base`, `model.ScopeDescription`.
- Produces (`view`):

```go
type DeviceEntryData struct {
	BasePage
	Error string
}

type DeviceConfirmData struct {
	BasePage
	Username    string
	UserCode    string // XXXX-XXXX
	DeviceName  string // already cleaned; empty means unknown
	RequesterIP string
	RequestedAt string // relative, e.g. "just now"
	Scopes      []string
	Confirm     components.ConfirmFactors
	Error       string
}

type DeviceDoneData struct {
	BasePage
	Approved bool
}
```

- Produces (`pages`): `DeviceEntry(view.DeviceEntryData)`, `DeviceConfirm(view.DeviceConfirmData)`, `DeviceDone(view.DeviceDoneData)`.

- [ ] **Step 1: Write the failing render tests** (`internal/view/pages/device_login_test.go`; copy the render-to-string helper and `BasePage` construction from `settings_test.go` or `page_title_test.go`)

```go
func TestDeviceConfirm_Content(t *testing.T) {
	html := renderDevice(t, DeviceConfirm(view.DeviceConfirmData{
		BasePage: testBasePage(), Username: "mk", UserCode: "BCDF-GHJK",
		DeviceName: `<script>alert(1)</script>`, RequesterIP: "203.0.113.7", RequestedAt: "just now",
		Scopes:  []string{"repo:write", "repo:read"},
		Confirm: components.ConfirmFactors{Password: true, Code: true},
	}))
	for _, want := range []string{
		"Authorize cz", "mk", "BCDF-GHJK", "unverified", "203.0.113.7", "just now",
		"Only continue if you just ran", "Untick to give less",
		`name="scope" value="repo:write"`, `name="scope" value="repo:read"`,
		`action="/login/device/approve"`, `name="password"`, `name="code"`,
		`name="action" value="approve"`, `name="action" value="deny"`,
		"Settings, Tokens",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("confirm page is missing %q", want)
		}
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("the device name must be escaped")
	}
	if strings.Count(html, "checked") < 2 {
		t.Error("every requested scope starts ticked")
	}
	if strings.Contains(html, `name="user_code"`) {
		t.Error("the code is held in a cookie, never in a form field")
	}
}

func TestDeviceConfirm_NoFactorsAccount(t *testing.T) {
	html := renderDevice(t, DeviceConfirm(view.DeviceConfirmData{BasePage: testBasePage(), UserCode: "BCDF-GHJK", Scopes: []string{"repo:read"}, Confirm: components.ConfirmFactors{Unavailable: true}}))
	if !strings.Contains(html, "no way to confirm") {
		t.Error("an account that cannot confirm should be told so")
	}
}

func TestDeviceEntryAndDone(t *testing.T) {
	entry := renderDevice(t, DeviceEntry(view.DeviceEntryData{BasePage: testBasePage(), Error: "That code isn't valid."}))
	for _, want := range []string{`action="/login/device"`, `name="user_code"`, "That code isn&#39;t valid."} {
		if !strings.Contains(entry, want) && !strings.Contains(entry, strings.ReplaceAll(want, "&#39;", "'")) {
			t.Errorf("entry page is missing %q", want)
		}
	}
	if !strings.Contains(renderDevice(t, DeviceDone(view.DeviceDoneData{BasePage: testBasePage(), Approved: true})), "return to your terminal") {
		t.Error("approved page should send the user back to the terminal")
	}
	if !strings.Contains(renderDevice(t, DeviceDone(view.DeviceDoneData{BasePage: testBasePage()})), "denied") {
		t.Error("denied page should say so")
	}
}
```

Define `renderDevice(t, templ.Component) string` and `testBasePage()` in the test file if the package has no equivalents (look at how `settings_test.go` builds a page). Run `go test ./internal/view/pages -run Device -v` — Expected: compile failure.

- [ ] **Step 2: View models** — `internal/view/viewmodels_device.go` with the three structs above (package `view`, import `components`).

- [ ] **Step 3: Templ** — `internal/view/pages/device_login.templ`, following `oauth_authorize.templ` for tokens (`rounded-md border border-border bg-card p-6`, `font-mono text-[11px] … uppercase tracking-wider` eyebrow, `components.Button(...)`):

```templ
package pages

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
)

templ DeviceEntry(data view.DeviceEntryData) {
	@layout.Base(data.BasePage, "Connect a device") {
		<div class="max-w-md mx-auto py-12">
			<div class="rounded-md border border-border bg-card p-6 space-y-5">
				<div>
					<p class="font-mono text-[11px] text-muted-foreground uppercase tracking-wider mb-1">Device login</p>
					<h1 class="text-xl font-semibold tracking-tight">Connect a device</h1>
					<p class="mt-2 text-[13px] text-muted-foreground">Enter the code shown in your terminal.</p>
				</div>
				if data.Error != "" {
					<div role="alert" class="border border-destructive/40 bg-destructive/10 text-destructive rounded-md px-3 py-2 text-[12.5px]">{ data.Error }</div>
				}
				<form method="POST" action="/login/device" class="space-y-4">
					<input name="user_code" type="text" required autocomplete="off" autocapitalize="characters" spellcheck="false" maxlength="9" placeholder="XXXX-XXXX" aria-label="Device code" class="h-11 w-full font-mono text-center text-xl tracking-widest uppercase bg-background border border-border rounded-md px-3 text-foreground focus:outline-hidden focus:border-ring"/>
					@components.Button(components.ButtonDefault, components.ButtonSizeDefault, templ.Attributes{"type": "submit"}) {
						Continue
					}
				</form>
			</div>
		</div>
	}
}

templ DeviceConfirm(data view.DeviceConfirmData) {
	@layout.Base(data.BasePage, "Authorize cz") {
		<div class="max-w-md mx-auto py-12">
			<div class="rounded-md border border-border bg-card p-6 space-y-5">
				<div>
					<p class="font-mono text-[11px] text-muted-foreground uppercase tracking-wider mb-1">Device login</p>
					<h1 class="text-xl font-semibold tracking-tight">Authorize cz</h1>
					if data.Username != "" {
						<p class="mt-1 text-[13px] text-muted-foreground">Signed in as { data.Username }</p>
					}
				</div>
				<div role="note" class="border border-warning/40 bg-warning/10 text-warning rounded-md px-3 py-2 text-[12.5px]">
					Only continue if you just ran <span class="font-mono">cz auth login</span> and this code matches your terminal.
				</div>
				<dl class="rounded-md border border-border divide-y divide-border text-[13px]">
					<div class="flex items-center justify-between gap-3 px-3 py-2"><dt class="text-muted-foreground">Code</dt><dd class="font-mono tracking-widest">{ data.UserCode }</dd></div>
					<div class="flex items-center justify-between gap-3 px-3 py-2">
						<dt class="text-muted-foreground">Device</dt>
						<dd>
							if data.DeviceName != "" {
								{ data.DeviceName }
							} else {
								Unknown
							}
							<span class="ml-1.5 text-[11px] text-muted-foreground border border-border rounded px-1.5 py-0.5">unverified</span>
						</dd>
					</div>
					<div class="flex items-center justify-between gap-3 px-3 py-2"><dt class="text-muted-foreground">Requested from</dt><dd>{ data.RequesterIP }, { data.RequestedAt }</dd></div>
				</dl>
				if data.Error != "" {
					<div role="alert" class="border border-destructive/40 bg-destructive/10 text-destructive rounded-md px-3 py-2 text-[12.5px]">{ data.Error }</div>
				}
				<form method="POST" action="/login/device/approve" class="space-y-4">
					<fieldset class="space-y-2">
						<legend class="text-[12px] font-medium text-muted-foreground mb-2">Access this token will have</legend>
						for _, s := range data.Scopes {
							<label class="flex items-start gap-3 rounded-md border border-border px-3 py-2.5 text-[13px] cursor-pointer">
								<input type="checkbox" name="scope" value={ s } checked class="mt-1"/>
								<span>
									<span class="block font-mono">{ s }</span>
									<span class="block text-[12px] text-muted-foreground">{ model.ScopeDescription(s) }</span>
								</span>
							</label>
						}
						<p class="text-[11.5px] text-muted-foreground">Untick to give less. You can't add access here.</p>
					</fieldset>
					if data.Confirm.Any() {
						<div class="space-y-2">
							<p class="text-[12px] font-medium text-muted-foreground">Confirm it's you</p>
							<div class="flex flex-wrap items-end gap-3">
								@components.ConfirmFields("device", data.Confirm, true)
							</div>
						</div>
					}
					<div class="flex items-center gap-2">
						@components.Button(components.ButtonSuccess, components.ButtonSizeDefault, templ.Attributes{"name": "action", "value": "approve", "type": "submit"}) {
							Authorize cz
						}
						@components.Button(components.ButtonOutline, components.ButtonSizeDefault, templ.Attributes{"name": "action", "value": "deny", "type": "submit", "formnovalidate": true}) {
							Deny
						}
					</div>
				</form>
				<p class="text-[11px] text-muted-foreground">You can revoke this token any time under Settings, Tokens.</p>
			</div>
		</div>
	}
}

templ DeviceDone(data view.DeviceDoneData) {
	@layout.Base(data.BasePage, "Device login") {
		<div class="max-w-md mx-auto py-12">
			<div class="rounded-md border border-border bg-card p-6 space-y-2">
				if data.Approved {
					<h1 class="text-xl font-semibold tracking-tight">Device authorized</h1>
					<p class="text-[13px] text-muted-foreground">You can close this tab and return to your terminal.</p>
				} else {
					<h1 class="text-xl font-semibold tracking-tight">Request denied</h1>
					<p class="text-[13px] text-muted-foreground">The login was denied. Nothing was authorized.</p>
				}
			</div>
		</div>
	}
}
```

Match the real button variant names (`ButtonDefault` may be `ButtonPrimary`; read `internal/view/components/button.templ`), and check whether the `warning` token classes exist in `tailwind/input.css` (use what other banners in `internal/view` use). `go` imports unused? Templ does not tolerate unused imports: remove any the final file does not use.

- [ ] **Step 4: Generate and test**

Run: `make generate-templ`, then `go test ./internal/view/... -run 'Device' -v` — Expected: PASS. Check `git status`: only the `.templ`, the generated `_templ.go`, the view-model and the test are new/changed (grep the generated file for `Authorize cz`; `templ generate`'s update count is unreliable).

```bash
git add internal/view
git commit -m "feat(auth): device login pages"
```

---

### Task 3: Handlers, routes and flow tests

**Files:**
- Create: `internal/handler/page_device_handler.go`, `internal/router/device_approval_test.go`
- Modify: `internal/router/router.go`

**Interfaces:**
- Consumes: Task 1 (`UserLimiter`, audit constants, `NotifyApproved`), Task 2 (pages), existing `DeviceGrantService.Lookup/Approve/Deny`, `Reauth.Confirm`, `confirmationFrom`, `reauthRefusal`, `confirmFactors`, `pages.SettingsErrorMessage`, `basePage`.
- Produces: `GET /login/device`, `POST /login/device`, `GET /login/device/confirm`, `POST /login/device/approve`.

- [ ] **Step 1: Read before writing** — `StartProviderSignIn` and `finishProviderSignIn` in `internal/handler/reauth.go`: confirm `return_to` accepts `/login/device/confirm`. Read `internal/handler/page_auth_handler.go` for how a page reads claims, sets cookies and uses `safeNextPath`, (`return_to` goes through `safeNextPath`, so any same-site path such as `/login/device/confirm` already works; no allow-list change is needed, but keep a test-free note in the report that you checked).

- [ ] **Step 2: Failing flow tests** (`internal/router/device_approval_test.go`, package `router_test`, using `newTestRouter`, `makeJWT`, `browserRequest(method, path, session, form)`, `serve`, `countRows`, `takeAudit`, `testutil.EnableTOTP`/`TOTPCode`/`TestTOTPSecret`, `deviceTestIP`). One helper creates a user with a password and a pending grant:

```go
type deviceFlow struct {
	h       http.Handler
	svc     *service.Services
	db      *sql.DB
	userID  int64
	session string
	dc      *service.DeviceCode
}

func newDeviceFlow(t *testing.T, scopes ...string) deviceFlow {
	t.Helper()
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	uid, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	ip := deviceTestIP()
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })
	dc, err := svc.DeviceGrant.Create(context.Background(), ip, scopes, "mk-laptop")
	if err != nil {
		t.Fatal(err)
	}
	return deviceFlow{h, svc, db, uid, makeJWT(t, uid, "testpw_"+suffix), dc}
}

// enter submits the code and returns the cookie that carries it to later requests.
func (f deviceFlow) enter(t *testing.T, code string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	rec := serve(f.h, browserRequest(http.MethodPost, "/login/device", f.session, url.Values{"user_code": {code}}))
	for _, c := range rec.Result().Cookies() {
		if c.Name == "cz_device_code" && c.Value != "" {
			return rec, c
		}
	}
	return rec, nil
}
```

Tests (each starts from `newDeviceFlow`; request `repo:read` and `repo:write` unless noted):

1. `TestDeviceLogin_SignedOutVisitGoesToSignInAndIgnoresTheCode`: unauthenticated `GET /login/device?user_code=BCDF-GHJK` → 303 whose `Location` is `/login?next=%2Flogin%2Fdevice` (the code is not in `next`); signed-in `GET /login/device?user_code=<real code>` → 200, body does not contain the code and the input has no `value=`.
2. `TestDeviceLogin_EntryRejectsUnknownAndExpiredCodesAlike`: an unknown code and an expired one (`UPDATE device_grants SET expires_at = NOW() - interval '1 minute'`) both give `400` and the same message, set no cookie.
3. `TestDeviceLogin_EntryThenConfirmPage`: entering the real code (typed lower-case without the hyphen, to exercise normalization) → 303 to `/login/device/confirm` and a `cz_device_code` cookie that is HttpOnly with `Path=/login/device`; `GET /login/device/confirm` with that cookie → 200 showing the code, `mk-laptop`, both scopes ticked, `X-Frame-Options: DENY`, `Content-Security-Policy` containing `frame-ancestors 'none'`, `Cache-Control: no-store`. Without the cookie, `GET /login/device/confirm` → 303 to `/login/device`.
4. `TestDeviceLogin_ApproveNeedsConfirmation`: with the cookie, approve with no/wrong password → `403`, grant still `pending` (`svc.DeviceGrant.Lookup` succeeds); five wrong attempts then the right password → `429` (throttle); a fresh user with the right password → success: `200`, body "Device authorized", grant `approved` (`SELECT status`), cookie cleared (`MaxAge < 0`), `takeAudit(model.AuditActionDeviceApprove, uid)`.
5. `TestDeviceLogin_ApprovedGrantYieldsAToken`: after approving, poll (`svc.DeviceGrant.Poll` with the clock advanced via `SetClock(+1 min)` as the service tests do) → a `czp_` token whose scopes equal the ticked ones.
6. `TestDeviceLogin_ScopesOnlyNarrow`: submit `scope=repo:read` → token scopes `[repo:read]`; submit `scope=repo:read&scope=repo:admin&scope=issues:write` → only `repo:read` kept; submit only `scope=repo:admin` (or none) → `400` with the "keep at least one" message, no failed-confirmation attempt spent (`SELECT reauth_failures FROM users WHERE id=$1` is 0) and the grant still pending.
7. `TestDeviceLogin_Deny`: deny without a password → `200` "Request denied", grant `denied`, `takeAudit(model.AuditActionDeviceDeny, uid)`; polling → `ErrAccessDenied`.
8. `TestDeviceLogin_TwoFactor`: `testutil.EnableTOTP`; password alone → `403`; password plus `testutil.TOTPCode(t, testutil.TestTOTPSecret)` → success.
9. `TestDeviceLogin_EntryIsLimitedPerUser`: 50 `POST /login/device` with a bogus code → `400` each; the 51st → `429` with `Retry-After`; a different user is unaffected.
10. `TestDeviceLogin_TokensCannotUseThePages`: a PAT (`svc.AccessToken.Generate(ctx, uid, "t", []string{"repo:write"}, nil)`) as `Authorization: Bearer …` on `GET /login/device` and on `POST /login/device/approve` → `403`.

Run: `go test ./internal/router -run DeviceLogin -v` — Expected: FAIL (404s).

- [ ] **Step 3: Handler** — `internal/handler/page_device_handler.go`:

```go
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const (
	deviceCodeCookie = "cz_device_code"
	devicePath       = "/login/device"
)

// The typed code rides in a cookie, not a URL, so a provider sign-in can return to the confirm page.
func (h *Handler) setDeviceCookie(w http.ResponseWriter, userCode string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: deviceCodeCookie, Value: userCode, Path: devicePath, MaxAge: maxAge,
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func deviceFrameGuard(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
}

func formatUserCode(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// PageDeviceEntry handles GET /login/device. Any user_code in the query is ignored on purpose.
func (h *Handler) PageDeviceEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); !ok {
		http.Redirect(w, r, view.WithNext("/login", devicePath), http.StatusSeeOther)
		return
	}
	h.renderDeviceEntry(w, r, http.StatusOK, "")
}

func (h *Handler) renderDeviceEntry(w http.ResponseWriter, r *http.Request, status int, msg string) {
	deviceFrameGuard(w)
	h.setDeviceCookie(w, "", -1)
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	h.render(w, r, pages.DeviceEntry(view.DeviceEntryData{BasePage: basePage(r, h.Services), Error: msg}))
}

// DeviceLookup handles POST /login/device: the user types the code from their terminal.
func (h *Handler) DeviceLookup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	g, err := h.Services.DeviceGrant.Lookup(r.Context(), r.PostFormValue("user_code"))
	if errors.Is(err, service.ErrDeviceGrantNotFound) {
		h.renderDeviceEntry(w, r, http.StatusBadRequest, "That code isn't valid or has expired. Check your terminal and try again.")
		return
	}
	if err != nil {
		slog.Error("device login: look up code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.setDeviceCookie(w, g.UserCode, int(service.DeviceGrantTTL.Seconds()))
	http.Redirect(w, r, devicePath+"/confirm", http.StatusSeeOther)
}

// deviceGrantFromCookie returns the grant the user entered, or sends them back to the entry page.
func (h *Handler) deviceGrantFromCookie(w http.ResponseWriter, r *http.Request) (*model.DeviceGrant, bool) {
	c, err := r.Cookie(deviceCodeCookie)
	if err == nil {
		g, lerr := h.Services.DeviceGrant.Lookup(r.Context(), c.Value)
		if lerr == nil {
			return g, true
		}
		if !errors.Is(lerr, service.ErrDeviceGrantNotFound) {
			slog.Error("device login: look up code", "error", lerr)
		}
	}
	h.setDeviceCookie(w, "", -1)
	http.Redirect(w, r, devicePath, http.StatusSeeOther)
	return nil, false
}

func (h *Handler) renderDeviceConfirm(w http.ResponseWriter, r *http.Request, claims middleware.Claims, g *model.DeviceGrant, scopes []string, status int, msg string) {
	deviceFrameGuard(w)
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	h.render(w, r, pages.DeviceConfirm(view.DeviceConfirmData{
		BasePage: basePage(r, h.Services), Username: claims.Username, UserCode: formatUserCode(g.UserCode),
		DeviceName: g.DeviceName, RequesterIP: g.RequesterIP, RequestedAt: view.Ago(g.CreatedAt),
		Scopes: scopes, Confirm: h.confirmFactors(r, claims.UserID), Error: msg,
	}))
}

// PageDeviceConfirm handles GET /login/device/confirm.
func (h *Handler) PageDeviceConfirm(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFromContext(r.Context())
	g, ok := h.deviceGrantFromCookie(w, r)
	if !ok {
		return
	}
	h.renderDeviceConfirm(w, r, claims, g, g.Scopes, http.StatusOK, "")
}

// DeviceApprove handles POST /login/device/approve.
func (h *Handler) DeviceApprove(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	g, ok := h.deviceGrantFromCookie(w, r)
	if !ok {
		return
	}
	details := func(scopes []string) map[string]any {
		return map[string]any{"device": g.DeviceName, "ip": g.RequesterIP, "scopes": scopes}
	}

	if r.PostFormValue("action") == "deny" {
		if err := h.Services.DeviceGrant.Deny(r.Context(), g.UserCode, claims.UserID); err != nil && !errors.Is(err, service.ErrDeviceGrantNotFound) {
			slog.Error("device login: deny", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionDeviceDeny, model.AuditTargetUser, claims.UserID, claims.Username, details(nil))
		h.finishDevice(w, r, false)
		return
	}

	// Anything outside the request is dropped, so a forged scope (repo:admin included) can't widen it.
	var scopes []string
	for _, s := range r.PostForm["scope"] {
		if slices.Contains(g.Scopes, s) && !slices.Contains(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	if len(scopes) == 0 {
		h.renderDeviceConfirm(w, r, claims, g, g.Scopes, http.StatusBadRequest, "Keep at least one permission, or deny the request.")
		return
	}
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		status, code, refused := reauthRefusal(claims.UserID, err)
		if !refused {
			slog.Error("device login: confirm", "user_id", claims.UserID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.renderDeviceConfirm(w, r, claims, g, g.Scopes, status, pages.SettingsErrorMessage(code))
		return
	}
	if err := h.Services.DeviceGrant.Approve(r.Context(), g.UserCode, claims.UserID, scopes); err != nil {
		if errors.Is(err, service.ErrDeviceGrantNotFound) {
			h.renderDeviceEntry(w, r, http.StatusGone, "That request expired or was already answered. Run cz auth login again.")
			return
		}
		slog.Error("device login: approve", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionDeviceApprove, model.AuditTargetUser, claims.UserID, claims.Username, details(scopes))
	h.Services.DeviceGrant.NotifyApproved(claims.UserID, g.DeviceName, g.RequesterIP, scopes)
	h.finishDevice(w, r, true)
}

func (h *Handler) finishDevice(w http.ResponseWriter, r *http.Request, approved bool) {
	deviceFrameGuard(w)
	h.setDeviceCookie(w, "", -1)
	h.render(w, r, pages.DeviceDone(view.DeviceDoneData{BasePage: basePage(r, h.Services), Approved: approved}))
}
```

Adapt to reality: `view.Ago` (internal/view/mirror.go) is the relative-time helper ("just now", "5 minutes ago"); remove the unused `time` import; confirm `claims.Username` is a field of `middleware.Claims` (used that way in `page_settings_handler.go`); `view.WithNext` is the helper `Unauthorized` uses.

- [ ] **Step 4: Routes** — in `internal/router/router.go`, beside `/oauth/authorize`:

```go
	deviceEntryLimiter := middleware.NewUserLimiter(deviceEntryLimit, deviceEntryWindow)
	r.With(optAuthMW).Get("/login/device", h.PageDeviceEntry)
	r.With(authMW, deviceEntryLimiter.Middleware(h.RateLimited)).Post("/login/device", h.DeviceLookup)
	r.With(authMW).Get("/login/device/confirm", h.PageDeviceConfirm)
	r.With(authMW).Post("/login/device/approve", h.DeviceApprove)
```

with constants `deviceEntryLimit = 50` and `deviceEntryWindow = time.Hour` next to the other limits. Check that `/login/device` does not collide with the existing `/login` routes and that CSRF protects the two POSTs (they use the form `csrf_token`; the layout adds it to forms — verify in the rendered page via the tests, which post with the token through `browserRequest`).

- [ ] **Step 5: Run, then commit**

Run: `go test ./internal/router ./internal/handler ./internal/view/... ./internal/middleware ./internal/service -run 'Device|UserLimiter' -race -v` (no SKIP), `go build ./...`, `gofmt -l internal`, `go vet ./internal/...`, `make lint`. Expected: PASS.

```bash
git add internal/handler internal/router internal/view
git commit -m "feat(auth): device login approval flow"
```

---

### Task 4: Docs and full verification

**Files:**
- Modify: `docs/access-control.md`, `docs/api-reference.md`

- [ ] **Step 1: `docs/access-control.md`** — read the existing sections first, then:
  1. In the table at the top add a row: `| Device login | `cz auth login`: code typed at `/login/device`, approved with the confirmation factors, token minted at the first poll | `/login/device`, `/api/auth/device/*` |`.
  2. In "Confirming sensitive actions", add a bullet: "approving a device login on `/login/device/confirm` (`POST /login/device/approve` with `action=approve`; denying needs nothing);".
  3. A new `### Device login` section (after "Confirming sensitive actions"): the flow in order (code request, typed code, cookie `cz_device_code` — HttpOnly, `Path=/login/device`, 15 minutes, never a URL; confirm page; approve; first poll mints the PAT), what the page shows, scope narrowing rules, the entry limit (50 per hour per user, per instance), the audit events and the email notice, that sessions only reach the pages (PATs get `403`), and the **threat model** table from ticket 07 (phishing with its residual risk stated plainly, guessing, polling brute force and leaked device code, replay, `repo:admin`, spoofed device name, links that carry the code) plus two more rows: "cross-site POST to `/api/auth/device/code` can use up one IP's grant budget (5 live grants, 20 requests per hour) for up to an hour; low value, accepted" and "the code cookie could be planted from a sibling subdomain; approving still needs the password and the page always shows the code and device".
  4. In the authorization matrix (Authentication Endpoints / User-Scoped Endpoints), add rows for the four browser routes (session required; `POST /login/device` limited per user) and the two JSON endpoints (no auth).
- [ ] **Step 2: `docs/api-reference.md`** — under the Device login section 07a added, add a short "Browser pages" table: `GET /login/device` (optional auth; redirects to sign-in), `POST /login/device` (session; `user_code`; 303 to the confirm page or 400 with one generic message), `GET /login/device/confirm` (session; needs the cookie), `POST /login/device/approve` (session; `action=approve|deny`, `scope` repeated, `password`/`code`/`email_code`; `403`/`429` as `POST /oauth/authorize`). Say that these are HTML pages for people, not API.
- [ ] **Step 3: Full verification and commit**

Run `gofmt -l internal`, `go vet ./...`, `make lint`, `go test ./... -race` with `TEST_DATABASE_DSN` set. Expected: all PASS (list any failure that predates this branch, with evidence).

```bash
git add docs/access-control.md docs/api-reference.md
git commit -m "docs(auth): document the device login approval flow and its threat model"
```

(The 07b ticket file lives on `feat/cz-device-login`; tick it in a follow-up once that PR is on `main`.)

---

## Self-Review

- **Spec coverage (07b + 07):** pages and routes (Tasks 2–3); sign-in redirect that never carries the code (Task 3 test 1); entry limit 50/h per user and one generic message (Task 1 limiter, Task 3 tests 2 and 9); confirm page layout and unverified device name (Task 2); `Reauth.Confirm` with all factor types through `ConfirmFields`, shared throttle, scope check before the attempt, narrowing only, `repo:admin` ignored (Task 3 tests 4, 6, 8); framing guard (Task 3 test 3); audit approve/deny/token-create and the email notice (Tasks 1, 3); provider round-trip returning to the confirm page with the grant intact — via the cookie and the `return_to` check in Task 3 Step 1; PATs excluded (test 10); docs and threat model (Task 4).
- **Deviations from the ticket text:** `user.token.create` is written by the token handler (needs the request), not the 07a service; the confirm step is its own page (`GET /login/device/confirm`) instead of the response to the entry POST, so the provider sign-in can return to it. Both keep the behaviour the ticket asks for.
- **Placeholders:** none; places that depend on repo facts (button variant names, relative-time helper, audit signature, `return_to` allow-list) say what to read and what to do with each outcome.
- **Type consistency:** `DeviceConfirmData`, `DeviceEntryData`, `DeviceDoneData`, `UserLimiter`, `NotifyApproved(userID, deviceName, ip, scopes)` and the cookie name `cz_device_code` are used identically in every task.

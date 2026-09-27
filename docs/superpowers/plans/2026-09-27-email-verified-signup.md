# Email-Verified Signup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When SMTP is configured, `/register` takes only an email and always answers "Check your inbox". The account is created from an emailed single-use link, so the form no longer reveals whether an email has an account.

**Architecture:**
- A `signup_tokens` table holds one hashed link per email; an upsert issues links and throttles them.
- `UserStore.CreateFromSignupToken` claims the link and creates the user in one transaction, mirroring `CreateFromInvitation`.
- `SignupService` generates tokens and sends mail through a `SignupMailer` interface that `EmailService` implements.
- `PageRegister`/`PageRegisterSubmit` switch to the email-only flow when `Signup.Enabled()`. New `/register/complete/{token}` handlers mirror the invite handlers.

**Tech Stack:** Go, chi, PostgreSQL (pgx stdlib), Templ, Tailwind.

**Spec:** `docs/superpowers/specs/2026-09-26-email-verified-signup-design.md`

**Deviation from spec:** the mailer interface is exported as `service.SignupMailer` and also carries `Enabled() bool`. Handler tests in package `handler_test` must be able to pass a fake, and `SignupService.Enabled()` needs a source that isn't SMTP config.

## Global Constraints

- Layering: stores (raw SQL) → services → handlers. Handlers call services only. `context.Context` is the first argument of every store and service method.
- Comments record only a *why* the code can't show, one line by default. No section or narration comments.
- Link lifetime: **24 hours**. Per-address throttle: **one link per 5 minutes**. Rate limit on both POST routes: `accountCreationLimit` / `accountCreationWindow` (10 per 15 minutes, already in `internal/router/router.go`).
- User-facing copy, verbatim:
  - "Check your inbox"
  - "Enter a valid email address"
  - "This link is no longer valid"
  - "That username is already taken" (from the existing `createAccountErrorMessage`)
- Accounts created by signup have `is_invited = FALSE`.
- Test DB: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable` (container `trusting-babbage-test-db`; `docker start trusting-babbage-test-db`).
  - After adding a migration, apply it with `CZ_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go run ./cmd/cloudzilla migrate`.
- Bash in this worktree: literal paths only; no `$(...)`, loops or heredocs. Write files with the Write/Edit tools.
- Commits: Conventional Commits. `git add` explicit paths, never `.claude/`. End every message with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After editing a `.templ` file, run `make generate-templ` and commit the regenerated `_templ.go`.

---

### Task 1: `signup_tokens` table and store

**Files:**
- Create: `internal/db/migrations/076_signup_tokens.sql`
- Create: `internal/model/signup_token.go`
- Create: `internal/store/signup_token_store.go`
- Modify: `internal/store/stores.go` (add the `SignupToken` field and constructor call)
- Modify: `internal/db/migrate_test.go` (append a test)
- Test: `internal/store/signup_token_store_test.go`

**Interfaces:**
- Produces:
  - `store.ErrSignupTokenUnusable error`
  - `const usableSignupTokenCond string` (package `store`, used by Task 2)
  - `store.NewSignupTokenStore(db *sql.DB) *store.SignupTokenStore`
  - `(*SignupTokenStore).Issue(ctx context.Context, email, tokenHash string, expiresAt time.Time) (bool, error)`
  - `(*SignupTokenStore).GetUsableByHash(ctx context.Context, tokenHash string) (*model.SignupToken, error)`
  - `model.SignupToken{ID int64; Email string; ExpiresAt time.Time}`
  - `Stores.SignupToken *SignupTokenStore`

- [ ] **Step 1: Write the failing migration test** — append to `internal/db/migrate_test.go`:

```go
func TestSignupTokensMigration_OneRowPerEmailIgnoringCase(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	testutil.Exec(t, db, `INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ('h1', 'a@test.invalid', NOW())`)

	_, err := db.Exec(`INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ('h2', 'A@Test.Invalid', NOW())`)

	if err == nil {
		t.Error("want a unique violation for an email that differs only by case")
	}
}
```

- [ ] **Step 2: Write the failing store tests** — create `internal/store/signup_token_store_test.go`:

```go
package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func cleanupSignupTokens(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM signup_tokens WHERE lower(email) = lower($1)`, email) })
}

func issueSignupToken(t *testing.T, s *store.SignupTokenStore, email, hash string) bool {
	t.Helper()
	issued, err := s.Issue(context.Background(), email, hash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return issued
}

func TestSignupTokenStore_Issue_WithinWindow_Throttles(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignupTokens(t, db, email)
	s := store.NewSignupTokenStore(db)

	if !issueSignupToken(t, s, email, "first_"+suffix) {
		t.Fatal("the first request must issue a link")
	}
	if issueSignupToken(t, s, email, "second_"+suffix) {
		t.Error("a second request within 5 minutes must not issue a link")
	}
	if _, err := s.GetUsableByHash(context.Background(), "first_"+suffix); err != nil {
		t.Errorf("a throttled request must keep the first link: %v", err)
	}
}

func TestSignupTokenStore_Issue_AfterWindow_ReplacesLink(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignupTokens(t, db, email)
	s := store.NewSignupTokenStore(db)

	issueSignupToken(t, s, email, "first_"+suffix)
	testutil.Exec(t, db, `UPDATE signup_tokens SET created_at = NOW() - INTERVAL '6 minutes' WHERE lower(email) = lower($1)`, email)
	if !issueSignupToken(t, s, email, "second_"+suffix) {
		t.Fatal("a request after 5 minutes must issue a new link")
	}

	if _, err := s.GetUsableByHash(context.Background(), "first_"+suffix); !errors.Is(err, store.ErrSignupTokenUnusable) {
		t.Errorf("a new link must replace the old one; got %v", err)
	}
	if _, err := s.GetUsableByHash(context.Background(), "second_"+suffix); err != nil {
		t.Errorf("the new link must be usable: %v", err)
	}
}

func TestSignupTokenStore_GetUsableByHash(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, db *sql.DB, suffix, email string)
		usable bool
	}{
		{"fresh", func(*testing.T, *sql.DB, string, string) {}, true},
		{"used", func(t *testing.T, db *sql.DB, _, email string) {
			testutil.Exec(t, db, `UPDATE signup_tokens SET used_at = NOW() WHERE lower(email) = lower($1)`, email)
		}, false},
		{"expired", func(t *testing.T, db *sql.DB, _, email string) {
			testutil.Exec(t, db, `UPDATE signup_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE lower(email) = lower($1)`, email)
		}, false},
		{"email registered in another case", func(t *testing.T, db *sql.DB, suffix, _ string) {
			testutil.SeedUser(t, db, suffix)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			suffix := testutil.UniqueSuffix(t)
			email := "TestUser_" + suffix + "@Test.Invalid"
			cleanupSignupTokens(t, db, email)
			s := store.NewSignupTokenStore(db)
			issueSignupToken(t, s, email, "hash_"+suffix)
			tc.setup(t, db, suffix, email)

			tok, err := s.GetUsableByHash(context.Background(), "hash_"+suffix)

			if tc.usable {
				if err != nil || tok.Email != email {
					t.Errorf("want usable link for %s, got %+v, %v", email, tok, err)
				}
				return
			}
			if !errors.Is(err, store.ErrSignupTokenUnusable) {
				t.Errorf("want ErrSignupTokenUnusable, got %v", err)
			}
		})
	}
}

func TestSignupTokenStore_GetUsableByHash_UnknownHash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	_, err := store.NewSignupTokenStore(db).GetUsableByHash(context.Background(), "no_such_hash_"+testutil.UniqueSuffix(t))
	if !errors.Is(err, store.ErrSignupTokenUnusable) {
		t.Errorf("want ErrSignupTokenUnusable, got %v", err)
	}
}
```

`testutil.SeedUser(t, db, suffix)` creates `testuser_<suffix>@test.invalid`. Issuing the link for `TestUser_<suffix>@Test.Invalid` checks that the lookup ignores case.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/store ./internal/db -run 'SignupToken' -count=1`
Expected: build failure (`undefined: store.NewSignupTokenStore`), or the migration test failing with `relation "signup_tokens" does not exist`.

- [ ] **Step 4: Write the migration** — create `internal/db/migrations/076_signup_tokens.sql`:

```sql
-- Links for email-first signup. Only each token's SHA-256 is stored; the raw
-- token exists only in the email. One row per address: a new request replaces
-- it, which also throttles mail to that inbox.
CREATE TABLE IF NOT EXISTS signup_tokens (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash  TEXT        NOT NULL UNIQUE,
    email       TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS signup_tokens_email_lower_key ON signup_tokens (lower(email));
```

- [ ] **Step 5: Write the model** — create `internal/model/signup_token.go`:

```go
package model

import "time"

// SignupToken is an emailed link that lets the mailbox owner finish creating an account.
type SignupToken struct {
	ID        int64     `db:"id"         json:"id"`
	Email     string    `db:"email"      json:"email"`
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
}
```

- [ ] **Step 6: Write the store** — create `internal/store/signup_token_store.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var ErrSignupTokenUnusable = errors.New("signup link is not usable")

// A link whose email already has an account is unusable, as for invitations.
const usableSignupTokenCond = `used_at IS NULL AND expires_at > NOW()
	AND NOT EXISTS (SELECT 1 FROM users WHERE lower(users.email) = lower(signup_tokens.email))`

// SignupTokenStore provides database operations for email-first signup links.
type SignupTokenStore struct {
	db *sql.DB
}

// NewSignupTokenStore creates a SignupTokenStore backed by the given database.
func NewSignupTokenStore(db *sql.DB) *SignupTokenStore {
	return &SignupTokenStore{db: db}
}

// Issue stores a link for email, replacing any earlier one. It writes nothing
// and returns false when the address was issued a link in the last 5 minutes.
func (s *SignupTokenStore) Issue(ctx context.Context, email, tokenHash string, expiresAt time.Time) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT ((lower(email))) DO UPDATE
		    SET token_hash = EXCLUDED.token_hash, email = EXCLUDED.email,
		        created_at = NOW(), expires_at = EXCLUDED.expires_at, used_at = NULL
		  WHERE signup_tokens.created_at < NOW() - INTERVAL '5 minutes'
		 RETURNING id`,
		tokenHash, email, expiresAt,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("signup token issue: %w", err)
	}
	return true, nil
}

func (s *SignupTokenStore) GetUsableByHash(ctx context.Context, tokenHash string) (*model.SignupToken, error) {
	t := &model.SignupToken{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, expires_at FROM signup_tokens WHERE token_hash = $1 AND `+usableSignupTokenCond,
		tokenHash,
	).Scan(&t.ID, &t.Email, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSignupTokenUnusable
	}
	if err != nil {
		return nil, fmt.Errorf("signup token get usable: %w", err)
	}
	return t, nil
}
```

- [ ] **Step 7: Wire the store** — in `internal/store/stores.go`:
  - add `SignupToken *SignupTokenStore` to the `Stores` struct directly below `Invitation       *InvitationStore`;
  - add `SignupToken: NewSignupTokenStore(database),` to the `return &Stores{...}` literal directly below `Invitation:       NewInvitationStore(database),`.

  Match the alignment of the neighbouring lines.

- [ ] **Step 8: Apply the migration and run the tests**

Run: `CZ_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go run ./cmd/cloudzilla migrate`
Expected: `applied migration file=076_signup_tokens.sql`

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/store ./internal/db -count=1`
Expected: `ok` for both packages.

- [ ] **Step 9: Commit**

```bash
git add internal/db/migrations/076_signup_tokens.sql internal/model/signup_token.go internal/store/signup_token_store.go internal/store/signup_token_store_test.go internal/store/stores.go internal/db/migrate_test.go
git commit -m "feat(signup): store hashed, throttled signup links" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Claim a signup link and create the user atomically

**Files:**
- Modify: `internal/store/user_store.go` (add `CreateFromSignupToken` directly below `CreateFromInvitation`)
- Test: `internal/store/signup_token_store_test.go` (append)

**Interfaces:**
- Consumes: `usableSignupTokenCond`, `ErrSignupTokenUnusable` (Task 1); the existing `insertUser(ctx, dbtx, *model.User) error` and `ErrEmailTaken` / `ErrUsernameTaken` in `user_store.go`.
- Produces: `(*UserStore).CreateFromSignupToken(ctx context.Context, u *model.User, tokenHash string) error`. On success it sets `u.Email` from the link, plus `u.ID`, `u.CreatedAt`, `u.UpdatedAt`.

- [ ] **Step 1: Write the failing tests** — append to `internal/store/signup_token_store_test.go`, and add `"github.com/mkappworks-dev/cloudzilla-app/internal/model"` to its imports:

```go
func newSignupUser(t *testing.T, db *sql.DB, username string) *model.User {
	t.Helper()
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE username = $1`, username) })
	return &model.User{Username: username, PasswordHash: "x"}
}

func TestUserStore_CreateFromSignupToken_ClaimsLinkAndUsesItsEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignupTokens(t, db, email)
	issueSignupToken(t, store.NewSignupTokenStore(db), email, "hash_"+suffix)
	u := newSignupUser(t, db, "signup_"+suffix)

	if err := store.NewUserStore(db).CreateFromSignupToken(context.Background(), u, "hash_"+suffix); err != nil {
		t.Fatalf("CreateFromSignupToken: %v", err)
	}

	if u.ID == 0 || u.Email != email {
		t.Errorf("want a created user with the link's email, got %+v", u)
	}
	if _, err := store.NewSignupTokenStore(db).GetUsableByHash(context.Background(), "hash_"+suffix); !errors.Is(err, store.ErrSignupTokenUnusable) {
		t.Errorf("a redeemed link must be unusable; got %v", err)
	}
}

// Covers the submit that loses a race: the link was usable when the page
// loaded but was redeemed before this call.
func TestUserStore_CreateFromSignupToken_UsedLink_CreatesNoUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignupTokens(t, db, email)
	issueSignupToken(t, store.NewSignupTokenStore(db), email, "hash_"+suffix)
	testutil.Exec(t, db, `UPDATE signup_tokens SET used_at = NOW() WHERE lower(email) = lower($1)`, email)
	u := newSignupUser(t, db, "signup_"+suffix)

	err := store.NewUserStore(db).CreateFromSignupToken(context.Background(), u, "hash_"+suffix)

	if !errors.Is(err, store.ErrSignupTokenUnusable) {
		t.Errorf("want ErrSignupTokenUnusable, got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = $1`, u.Username).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Errorf("a used link must not create a user; found %d", n)
	}
}

func TestUserStore_CreateFromSignupToken_UsernameTaken_LinkStaysUsable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignupTokens(t, db, email)
	issueSignupToken(t, store.NewSignupTokenStore(db), email, "hash_"+suffix)

	err := store.NewUserStore(db).CreateFromSignupToken(context.Background(), &model.User{Username: "testuser_" + suffix, PasswordHash: "x"}, "hash_"+suffix)

	if !errors.Is(err, store.ErrUsernameTaken) {
		t.Errorf("want ErrUsernameTaken, got %v", err)
	}
	if _, err := store.NewSignupTokenStore(db).GetUsableByHash(context.Background(), "hash_"+suffix); err != nil {
		t.Errorf("a failed create must leave the link usable: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/store -run CreateFromSignupToken -count=1`
Expected: build failure `CreateFromSignupToken undefined`.

- [ ] **Step 3: Implement** — in `internal/store/user_store.go`, directly below `CreateFromInvitation`:

```go
// CreateFromSignupToken claims the signup link and inserts u with the link's
// email in one transaction, so a failed insert leaves the link usable and
// concurrent submits can't both redeem it.
func (s *UserStore) CreateFromSignupToken(ctx context.Context, u *model.User, tokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("user create from signup token: begin: %w", err)
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx,
		`UPDATE signup_tokens SET used_at = NOW() WHERE token_hash = $1 AND `+usableSignupTokenCond+` RETURNING email`,
		tokenHash,
	).Scan(&u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSignupTokenUnusable
	}
	if err != nil {
		return fmt.Errorf("user create from signup token: claim: %w", err)
	}

	if err := insertUser(ctx, tx, u); err != nil {
		// The email was registered after the claim; same rule as usableSignupTokenCond.
		if errors.Is(err, ErrEmailTaken) {
			return ErrSignupTokenUnusable
		}
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run the tests**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/store -count=1`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/store/user_store.go internal/store/signup_token_store_test.go
git commit -m "feat(signup): redeem a signup link and create its user atomically" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Signup emails

**Files:**
- Modify: `internal/service/email_service.go` (add `Enabled`, `SendSignupLink`, `SendAccountExists`, `signupLinkEmail`, `accountExistsEmail`)
- Test: `internal/service/email_service_test.go` (create; package `service`, since it tests unexported builders)

**Interfaces:**
- Produces:
  - `(*EmailService).Enabled() bool`
  - `(*EmailService).SendSignupLink(to, link string) error`
  - `(*EmailService).SendAccountExists(to, loginURL string) error`

- [ ] **Step 1: Write the failing tests** — create `internal/service/email_service_test.go`:

```go
package service

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

func TestEmailService_Enabled(t *testing.T) {
	if NewEmailService(config.SMTPConfig{}).Enabled() {
		t.Error("no SMTP host must mean disabled")
	}
	if !NewEmailService(config.SMTPConfig{Host: "smtp.test"}).Enabled() {
		t.Error("an SMTP host must mean enabled")
	}
}

func TestSignupLinkEmail_LinksAndEscapes(t *testing.T) {
	_, body := signupLinkEmail("https://cz.test/register/complete/abc")
	if !strings.Contains(body, `href="https://cz.test/register/complete/abc"`) {
		t.Errorf("body must link to the completion page:\n%s", body)
	}
	_, body = signupLinkEmail(`https://cz.test/"><script>x</script>`)
	if strings.Contains(body, "<script>") {
		t.Errorf("link must be HTML-escaped:\n%s", body)
	}
}

func TestAccountExistsEmail_LinksToSignIn(t *testing.T) {
	_, body := accountExistsEmail("https://cz.test/login")
	if !strings.Contains(body, `href="https://cz.test/login"`) {
		t.Errorf("body must link to sign-in:\n%s", body)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/service -run 'EmailService_Enabled|SignupLinkEmail|AccountExistsEmail' -count=1`
Expected: build failure (`Enabled undefined`, `undefined: signupLinkEmail`).

- [ ] **Step 3: Implement** — append to `internal/service/email_service.go`:

```go
func (s *EmailService) Enabled() bool {
	return s.cfg.Host != ""
}

func (s *EmailService) SendSignupLink(to, link string) error {
	subject, body := signupLinkEmail(link)
	return s.Send(to, subject, body)
}

func (s *EmailService) SendAccountExists(to, loginURL string) error {
	subject, body := accountExistsEmail(loginURL)
	return s.Send(to, subject, body)
}

func signupLinkEmail(link string) (subject, body string) {
	return "Finish creating your Cloudzilla account", fmt.Sprintf(
		`<p>Someone asked to create a Cloudzilla account with this email address.</p>`+
			`<p><a href="%s">Choose a username and password</a> to finish. The link works once and expires in 24 hours.</p>`+
			`<p>If this wasn't you, ignore this email.</p>`,
		html.EscapeString(link))
}

func accountExistsEmail(loginURL string) (subject, body string) {
	return "You already have a Cloudzilla account", fmt.Sprintf(
		`<p>Someone asked to create a Cloudzilla account with this email address, but it already has one.</p>`+
			`<p><a href="%s">Sign in</a> instead.</p>`+
			`<p>If this wasn't you, ignore this email.</p>`,
		html.EscapeString(loginURL))
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/service -run 'EmailService_Enabled|SignupLinkEmail|AccountExistsEmail' -count=1`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/service/email_service.go internal/service/email_service_test.go
git commit -m "feat(signup): add signup-link and account-exists emails" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `SignupService`

**Files:**
- Create: `internal/service/signup_service.go`
- Modify: `internal/service/services.go`:
  - add `Signup *SignupService` below `Invitation *InvitationService` in the struct;
  - add `Signup: NewSignupService(stores.SignupToken, stores.User, emailSvc, cfg.Server.BaseURL),` below the `Invitation:` line in `New`.
- Test: `internal/service/signup_service_test.go` (package `service_test`)

**Interfaces:**
- Consumes:
  - `store.SignupTokenStore.Issue` / `GetUsableByHash` and `store.ErrSignupTokenUnusable` (Task 1)
  - `store.UserStore.CreateFromSignupToken` (Task 2)
  - `store.UserStore.GetByEmail`, existing; case-insensitive, wraps `sql.ErrNoRows`
  - `EmailService` methods (Task 3)
- Produces:
  - `service.ErrSignupTokenUnusable` (alias of the store error)
  - `service.SignupMailer` interface: `Enabled() bool; SendSignupLink(to, link string) error; SendAccountExists(to, loginURL string) error`
  - `service.NewSignupService(tokens *store.SignupTokenStore, users *store.UserStore, mailer SignupMailer, baseURL string) *SignupService`
  - `(*SignupService).Enabled() bool`
  - `(*SignupService).Request(ctx context.Context, email string) error`
  - `(*SignupService).GetUsable(ctx context.Context, token string) (*model.SignupToken, error)`
  - `(*SignupService).Complete(ctx context.Context, token, username, password string) (*model.User, error)`
  - `Services.Signup *SignupService`
  - Completion links look like `<baseURL without trailing slash>/register/complete/<64 hex chars>`.

- [ ] **Step 1: Write the failing tests** — create `internal/service/signup_service_test.go`:

```go
package service_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type sentSignupMail struct{ kind, to, url string }

type recordingSignupMailer struct{ sent []sentSignupMail }

func (m *recordingSignupMailer) Enabled() bool { return true }

func (m *recordingSignupMailer) SendSignupLink(to, link string) error {
	m.sent = append(m.sent, sentSignupMail{"link", to, link})
	return nil
}

func (m *recordingSignupMailer) SendAccountExists(to, loginURL string) error {
	m.sent = append(m.sent, sentSignupMail{"exists", to, loginURL})
	return nil
}

const signupBaseURL = "https://cz.test"

func newSignupSvc(db *sql.DB, mailer service.SignupMailer) *service.SignupService {
	return service.NewSignupService(store.NewSignupTokenStore(db), store.NewUserStore(db), mailer, signupBaseURL+"/")
}

func cleanupSignup(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM signup_tokens WHERE lower(email) = lower($1)`, email)
		testutil.Exec(t, db, `DELETE FROM users WHERE lower(email) = lower($1) AND username LIKE 'signup\_%'`, email)
	})
}

func requestLink(t *testing.T, svc *service.SignupService, mailer *recordingSignupMailer, email string) string {
	t.Helper()
	if err := svc.Request(context.Background(), email); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(mailer.sent) == 0 || mailer.sent[len(mailer.sent)-1].kind != "link" {
		t.Fatalf("want a signup link email, got %+v", mailer.sent)
	}
	link := mailer.sent[len(mailer.sent)-1].url
	const prefix = signupBaseURL + "/register/complete/"
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("link %q must start with %q", link, prefix)
	}
	return strings.TrimPrefix(link, prefix)
}

func TestSignupService_Request_NewEmail_SendsLinkStoredOnlyAsHash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}
	svc := newSignupSvc(db, mailer)

	token := requestLink(t, svc, mailer, email)

	if mailer.sent[0].to != email {
		t.Errorf("mail must go to %s, went to %s", email, mailer.sent[0].to)
	}
	tok, err := svc.GetUsable(context.Background(), token)
	if err != nil || tok.Email != email {
		t.Errorf("the emailed link must be usable for %s: %+v, %v", email, tok, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM signup_tokens WHERE token_hash = $1`, token).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Error("the raw token must never be stored")
	}
}

func TestSignupService_Request_ExistingEmailInOtherCase_SendsAccountExists(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	email := "TestUser_" + suffix + "@Test.Invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}

	if err := newSignupSvc(db, mailer).Request(context.Background(), email); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if len(mailer.sent) != 1 || mailer.sent[0].kind != "exists" || mailer.sent[0].url != signupBaseURL+"/login" {
		t.Errorf("want one account-exists mail linking to sign-in, got %+v", mailer.sent)
	}
}

func TestSignupService_Request_WithinThrottleWindow_SendsOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}
	svc := newSignupSvc(db, mailer)

	for range 2 {
		if err := svc.Request(context.Background(), email); err != nil {
			t.Fatalf("Request: %v", err)
		}
	}

	if len(mailer.sent) != 1 {
		t.Errorf("want 1 mail within 5 minutes, got %d", len(mailer.sent))
	}
}

func TestSignupService_Complete_CreatesOrdinaryUserWithLinkEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}
	svc := newSignupSvc(db, mailer)
	token := requestLink(t, svc, mailer, email)

	u, err := svc.Complete(context.Background(), token, "signup_"+suffix, "password123")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if u.Email != email || u.IsInvited {
		t.Errorf("want an ordinary user with the link's email, got %+v", u)
	}
	if _, err := svc.Complete(context.Background(), token, "signup2_"+suffix, "password123"); !errors.Is(err, service.ErrSignupTokenUnusable) {
		t.Errorf("a redeemed link must be unusable; got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/service -run SignupService -count=1`
Expected: build failure `undefined: service.NewSignupService`.

- [ ] **Step 3: Implement** — create `internal/service/signup_service.go`:

```go
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const signupLinkTTL = 24 * time.Hour

var ErrSignupTokenUnusable = store.ErrSignupTokenUnusable

// SignupMailer sends the two emails of email-first signup.
type SignupMailer interface {
	Enabled() bool
	SendSignupLink(to, link string) error
	SendAccountExists(to, loginURL string) error
}

// SignupService runs email-first signup: an account is created only from a
// link mailed to its address, so /register never reveals whether one exists.
type SignupService struct {
	tokens  *store.SignupTokenStore
	users   *store.UserStore
	mailer  SignupMailer
	baseURL string
}

// NewSignupService creates a SignupService that builds links from baseURL.
func NewSignupService(tokens *store.SignupTokenStore, users *store.UserStore, mailer SignupMailer, baseURL string) *SignupService {
	return &SignupService{tokens: tokens, users: users, mailer: mailer, baseURL: strings.TrimRight(baseURL, "/")}
}

func (s *SignupService) Enabled() bool {
	return s.mailer.Enabled()
}

// Request mails email a signup link, or a sign-in reminder if it already has an
// account. It sends nothing if the address was mailed in the last 5 minutes.
func (s *SignupService) Request(ctx context.Context, email string) error {
	token, err := newSignupToken()
	if err != nil {
		return err
	}
	issued, err := s.tokens.Issue(ctx, email, hashSignupToken(token), time.Now().Add(signupLinkTTL))
	if err != nil || !issued {
		return err
	}
	_, err = s.users.GetByEmail(ctx, email)
	switch {
	case err == nil:
		return s.mailer.SendAccountExists(email, s.baseURL+"/login")
	case errors.Is(err, sql.ErrNoRows):
		return s.mailer.SendSignupLink(email, s.baseURL+"/register/complete/"+token)
	default:
		return err
	}
}

func (s *SignupService) GetUsable(ctx context.Context, token string) (*model.SignupToken, error) {
	return s.tokens.GetUsableByHash(ctx, hashSignupToken(token))
}

// Complete creates the account for a usable link. It returns
// ErrSignupTokenUnusable if the link was used, expired, or its email
// registered since it was loaded.
func (s *SignupService) Complete(ctx context.Context, token, username, password string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u := &model.User{Username: username, PasswordHash: string(hash)}
	if err := s.users.CreateFromSignupToken(ctx, u, hashSignupToken(token)); err != nil {
		return nil, err
	}
	return u, nil
}

func newSignupToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate signup token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashSignupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Wire the service** — make the two `internal/service/services.go` edits listed under **Files**.

- [ ] **Step 5: Run the tests and build**

Run: `go build ./... && TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/service -count=1`
Expected: `ok`

- [ ] **Step 6: Commit**

```bash
git add internal/service/signup_service.go internal/service/signup_service_test.go internal/service/services.go
git commit -m "feat(signup): add SignupService for email-first signup" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Signup pages

**Files:**
- Modify: `internal/view/viewmodels_auth.go` (append three view-models)
- Create: `internal/view/pages/signup.templ` (then `make generate-templ` creates `signup_templ.go`)
- Test: `internal/view/pages/signup_test.go`

**Interfaces:**
- Consumes: `model.SignupToken` (Task 1), `authCardHero` (in `internal/view/pages/login.templ`), `components.Input`, `components.Button`, `layout.Base`.
- Produces:
  - `view.RegisterEmailData{BasePage; Email, Error string}`
  - `view.RegisterCheckInboxData{BasePage}`
  - `view.RegisterCompleteData{BasePage; Signup *model.SignupToken; Username, Error string}` (a nil `Signup` renders the invalid-link page)
  - `pages.RegisterEmail(view.RegisterEmailData) templ.Component`
  - `pages.RegisterCheckInbox(view.RegisterCheckInboxData) templ.Component`
  - `pages.RegisterComplete(view.RegisterCompleteData) templ.Component`

- [ ] **Step 1: Write the failing tests** — create `internal/view/pages/signup_test.go`:

```go
package pages

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

func renderPage(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestRegisterEmail_OnlyAsksForEmail(t *testing.T) {
	body := renderPage(t, RegisterEmail(view.RegisterEmailData{Email: "a@test.invalid", Error: "Enter a valid email address"}))
	if !strings.Contains(body, `name="email"`) || strings.Contains(body, `name="password"`) {
		t.Errorf("want an email-only form:\n%s", body)
	}
	if !strings.Contains(body, "Enter a valid email address") || !strings.Contains(body, `value="a@test.invalid"`) {
		t.Errorf("want the error and the submitted email kept:\n%s", body)
	}
}

func TestRegisterCheckInbox_SaysCheckYourInbox(t *testing.T) {
	if body := renderPage(t, RegisterCheckInbox(view.RegisterCheckInboxData{})); !strings.Contains(body, "Check your inbox") {
		t.Errorf("want the check-inbox heading:\n%s", body)
	}
}

func TestRegisterComplete_UsableLink_ShowsEmailReadOnlyForm(t *testing.T) {
	body := renderPage(t, RegisterComplete(view.RegisterCompleteData{Signup: &model.SignupToken{Email: "a@test.invalid"}, Username: "alice"}))
	for _, want := range []string{`value="a@test.invalid"`, "readonly", `name="username"`, `value="alice"`, `name="password"`} {
		if !strings.Contains(body, want) {
			t.Errorf("completion form must contain %s:\n%s", want, body)
		}
	}
}

func TestRegisterComplete_NoSignup_ShowsInvalidLinkWithoutForm(t *testing.T) {
	body := renderPage(t, RegisterComplete(view.RegisterCompleteData{}))
	if !strings.Contains(body, "This link is no longer valid") || strings.Contains(body, `name="username"`) {
		t.Errorf("want the invalid-link page without a form:\n%s", body)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/view/pages -run 'RegisterEmail|RegisterCheckInbox|RegisterComplete' -count=1`
Expected: build failure `undefined: RegisterEmail` / `undefined: view.RegisterEmailData`.

- [ ] **Step 3: Add the view-models** — append to `internal/view/viewmodels_auth.go`:

```go
// RegisterEmailData holds template data for the email-first registration form.
type RegisterEmailData struct {
	BasePage
	Email string
	Error string
}

// RegisterCheckInboxData holds template data for the page shown after every email-first registration submit.
type RegisterCheckInboxData struct {
	BasePage
}

// RegisterCompleteData holds template data for finishing a signup from its emailed link.
// Signup is nil when the link is unusable.
type RegisterCompleteData struct {
	BasePage
	Signup   *model.SignupToken
	Username string
	Error    string
}
```

- [ ] **Step 4: Write the pages** — create `internal/view/pages/signup.templ`:

```templ
package pages

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
)

templ RegisterEmail(data view.RegisterEmailData) {
	@layout.Base(data.BasePage, "Create account") {
		<div class="max-w-md mx-auto mt-12 rounded-md border border-border bg-card p-6">
			@authCardHero("Cloudzilla", "Create your account", "We'll email you a link to finish signing up.")
			if data.Error != "" {
				<div class="mb-4 p-3 rounded-md border border-destructive/40 bg-destructive/10 text-destructive text-sm" role="alert">
					{ data.Error }
				</div>
			}
			<form method="POST" action="/register" class="space-y-4">
				<div class="space-y-1.5">
					<label for="email" class="block text-sm font-medium">Email</label>
					@components.Input(templ.Attributes{
						"type":      "email",
						"id":        "email",
						"name":      "email",
						"required":  "required",
						"autofocus": "autofocus",
						"value":     data.Email,
					})
				</div>
				@components.Button(components.ButtonDefault, components.ButtonSizeDefault, templ.Attributes{"type": "submit", "class": "w-full"}) {
					Continue
				}
			</form>
			<p class="mt-5 text-center text-[12px] text-muted-foreground">
				Already have an account?
				<a href="/login" class="text-foreground hover:underline">Sign in</a>
			</p>
		</div>
	}
}

templ RegisterCheckInbox(data view.RegisterCheckInboxData) {
	@layout.Base(data.BasePage, "Check your inbox") {
		<div class="max-w-md mx-auto mt-12 rounded-md border border-border bg-card p-6">
			@authCardHero("Cloudzilla", "Check your inbox", "We've emailed that address with the next step. If nothing arrives, check your spam folder or try again in a few minutes.")
			<p class="text-center text-[12px] text-muted-foreground">
				<a href="/login" class="text-foreground hover:underline">Back to sign in</a>
			</p>
		</div>
	}
}

templ RegisterComplete(data view.RegisterCompleteData) {
	@layout.Base(data.BasePage, "Create account") {
		<div class="max-w-md mx-auto mt-12 rounded-md border border-border bg-card p-6">
			if data.Signup == nil {
				@authCardHero("Cloudzilla", "This link is no longer valid", "Request a new one from the sign-up page.")
				<p class="text-center text-[12px] text-muted-foreground">
					<a href="/register" class="text-foreground hover:underline">Sign up</a>
					or
					<a href="/login" class="text-foreground hover:underline">sign in</a>
				</p>
			} else {
				@authCardHero("Cloudzilla", "Finish creating your account", "Choose a username and password.")
				if data.Error != "" {
					<div class="mb-4 p-3 rounded-md border border-destructive/40 bg-destructive/10 text-destructive text-sm" role="alert">{ data.Error }</div>
				}
				<form method="POST" class="space-y-4">
					<div class="space-y-1.5">
						<label for="complete-email" class="block text-sm font-medium">Email</label>
						@components.Input(templ.Attributes{
							"type":     "email",
							"id":       "complete-email",
							"value":    data.Signup.Email,
							"readonly": "readonly",
							"class":    "bg-muted text-muted-foreground cursor-not-allowed",
						})
					</div>
					<div class="space-y-1.5">
						<label for="complete-username" class="block text-sm font-medium">Username</label>
						@components.Input(templ.Attributes{
							"type":      "text",
							"id":        "complete-username",
							"name":      "username",
							"required":  "required",
							"autofocus": "autofocus",
							"value":     data.Username,
						})
					</div>
					<div class="space-y-1.5">
						<label for="complete-password" class="block text-sm font-medium">Password</label>
						@components.Input(templ.Attributes{
							"type":      "password",
							"id":        "complete-password",
							"name":      "password",
							"required":  "required",
							"minlength": "8",
						})
					</div>
					@components.Button(components.ButtonDefault, components.ButtonSizeDefault, templ.Attributes{"type": "submit", "class": "w-full"}) {
						Create account
					}
				</form>
			}
		</div>
	}
}
```

- [ ] **Step 5: Generate and check formatting**

Run: `make generate-templ`
Expected: `Complete [ updates=1 ...]`, and `internal/view/pages/signup_templ.go` exists.

Run: `~/go/bin/templ fmt -stdout internal/view/pages/signup.templ`
Expected: output identical to the file. If it differs, run `~/go/bin/templ fmt internal/view/pages/signup.templ` and `make generate-templ` again.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/view/... -count=1`
Expected: `ok`

- [ ] **Step 7: Commit**

```bash
git add internal/view/viewmodels_auth.go internal/view/pages/signup.templ internal/view/pages/signup_templ.go internal/view/pages/signup_test.go
git commit -m "feat(signup): add email-first signup pages" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Email-only `/register` when SMTP is configured

**Files:**
- Create: `internal/handler/signup_handler.go` (`requestSignup`, `validEmail`)
- Modify: `internal/handler/register_handler.go` (branch `PageRegister` and `PageRegisterSubmit` on `Signup.Enabled()`)
- Modify: `internal/handler/auth_handler_test.go` (`newAuthHandler` gets a disabled `Signup`)
- Modify: `internal/handler/register_handler_test.go` (extract `setAllowRegistration`)
- Test: `internal/handler/signup_handler_test.go` (create)

**Interfaces:**
- Consumes:
  - `service.SignupService`, `service.SignupMailer`, `service.NewSignupService`, `Services.Signup` (Task 4)
  - `pages.RegisterEmail`, `pages.RegisterCheckInbox`, `view.RegisterEmailData`, `view.RegisterCheckInboxData` (Task 5)
  - `concurrency.Go(label string, fn func())` from `internal/concurrency`
- Produces (test helpers in package `handler_test`, used by Task 7):
  - `newFakeSignupMailer() *fakeSignupMailer`
  - `(*fakeSignupMailer).next(t) sentSignupMail`
  - `newSignupHandler(db *sql.DB, mailer service.SignupMailer) *handler.Handler`
  - `signupRouter(h *handler.Handler) *chi.Mux`
  - `cleanupSignup(t, db, email)`
  - `requestSignupLink(t, db, h, mailer, email) (token string)`
  - `setAllowRegistration(t, db, value string)`
  - `const signupBaseURL = "https://cz.test"`

- [ ] **Step 1: Keep existing handler tests on the full form** — in `internal/handler/auth_handler_test.go`, inside `newAuthHandler`, add a disabled signup service to the `svc` literal:

```go
		Signup:      service.NewSignupService(store.NewSignupTokenStore(db), stores.User, service.NewEmailService(config.SMTPConfig{}), ""),
```

- [ ] **Step 2: Generalise the registration setting helper** — in `internal/handler/register_handler_test.go`, replace the `enableRegistration` function with:

```go
// setAllowRegistration sets allow_registration for the test and restores the
// previous value, or its absence, afterwards.
func setAllowRegistration(t *testing.T, db *sql.DB, value string) {
	t.Helper()
	settings := store.NewSiteSettingStore(db)
	prev, err := settings.Get(context.Background(), "allow_registration")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM site_settings WHERE key = 'allow_registration'`) })
	case err != nil:
		t.Fatalf("read allow_registration: %v", err)
	default:
		t.Cleanup(func() {
			testutil.Exec(t, db, `UPDATE site_settings SET value = $1 WHERE key = 'allow_registration'`, prev)
		})
	}
	if err := settings.Set(context.Background(), "allow_registration", value); err != nil {
		t.Fatalf("set allow_registration: %v", err)
	}
}

func enableRegistration(t *testing.T, db *sql.DB) {
	t.Helper()
	setAllowRegistration(t, db, "true")
}
```

- [ ] **Step 3: Write the failing tests** — create `internal/handler/signup_handler_test.go`:

```go
package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const signupBaseURL = "https://cz.test"

type sentSignupMail struct{ kind, to, url string }

// fakeSignupMailer reports mail on a channel because /register sends it from a goroutine.
type fakeSignupMailer struct{ sent chan sentSignupMail }

func newFakeSignupMailer() *fakeSignupMailer {
	return &fakeSignupMailer{sent: make(chan sentSignupMail, 16)}
}

func (m *fakeSignupMailer) Enabled() bool { return true }

func (m *fakeSignupMailer) SendSignupLink(to, link string) error {
	m.sent <- sentSignupMail{"link", to, link}
	return nil
}

func (m *fakeSignupMailer) SendAccountExists(to, loginURL string) error {
	m.sent <- sentSignupMail{"exists", to, loginURL}
	return nil
}

func (m *fakeSignupMailer) next(t *testing.T) sentSignupMail {
	t.Helper()
	select {
	case s := <-m.sent:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no signup email was sent")
		return sentSignupMail{}
	}
}

func (m *fakeSignupMailer) assertNoneSent(t *testing.T) {
	t.Helper()
	select {
	case s := <-m.sent:
		t.Errorf("want no email, got %+v", s)
	case <-time.After(200 * time.Millisecond):
	}
}

func newSignupHandler(db *sql.DB, mailer service.SignupMailer) *handler.Handler {
	h := newAuthHandler(db)
	h.Services.Signup = service.NewSignupService(store.NewSignupTokenStore(db), store.NewUserStore(db), mailer, signupBaseURL)
	return h
}

func signupRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/register", h.PageRegister)
	r.Post("/register", h.PageRegisterSubmit)
	r.Get("/register/complete/{token}", h.PageRegisterComplete)
	r.Post("/register/complete/{token}", h.PageRegisterCompleteSubmit)
	return r
}

func serveSignup(h *handler.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rr := httptest.NewRecorder()
	signupRouter(h).ServeHTTP(rr, req)
	return rr
}

func cleanupSignup(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM signup_tokens WHERE lower(email) = lower($1)`, email)
		testutil.Exec(t, db, `DELETE FROM users WHERE lower(email) = lower($1) AND username LIKE 'signup\_%'`, email)
	})
}

// requestSignupLink submits /register for email and returns the token from the emailed link.
func requestSignupLink(t *testing.T, db *sql.DB, h *handler.Handler, mailer *fakeSignupMailer, email string) string {
	t.Helper()
	enableRegistration(t, db)
	serveSignup(h, http.MethodPost, "/register", url.Values{"email": {email}})
	m := mailer.next(t)
	const prefix = signupBaseURL + "/register/complete/"
	if m.kind != "link" || !strings.HasPrefix(m.url, prefix) {
		t.Fatalf("want a signup link email, got %+v", m)
	}
	return strings.TrimPrefix(m.url, prefix)
}

func TestPageRegister_SignupEnabled_AsksOnlyForEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	enableRegistration(t, db)

	body := serveSignup(newSignupHandler(db, newFakeSignupMailer()), http.MethodGet, "/register", nil).Body.String()

	if !strings.Contains(body, `name="email"`) || strings.Contains(body, `name="password"`) {
		t.Errorf("want the email-only form:\n%s", body)
	}
}

// The heart of the design: nothing in the response depends on whether the
// address has an account.
func TestPageRegisterSubmit_SignupEnabled_SameResponseForNewAndExistingEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	existing := "TestUser_" + suffix + "@Test.Invalid"
	fresh := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, existing)
	cleanupSignup(t, db, fresh)
	enableRegistration(t, db)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)

	newRR := serveSignup(h, http.MethodPost, "/register", url.Values{"email": {fresh}})
	existingRR := serveSignup(h, http.MethodPost, "/register", url.Values{"email": {existing}})

	if newRR.Code != existingRR.Code || newRR.Body.String() != existingRR.Body.String() {
		t.Errorf("responses differ:\nnew (%d):\n%s\nexisting (%d):\n%s", newRR.Code, newRR.Body, existingRR.Code, existingRR.Body)
	}
	if !strings.Contains(newRR.Body.String(), "Check your inbox") {
		t.Errorf("want the check-inbox page:\n%s", newRR.Body)
	}
	kinds := map[string]string{}
	for range 2 {
		m := mailer.next(t)
		kinds[strings.ToLower(m.to)] = m.kind
	}
	if kinds[strings.ToLower(fresh)] != "link" || kinds[strings.ToLower(existing)] != "exists" {
		t.Errorf("want a link for the new address and an account-exists mail for the existing one, got %v", kinds)
	}
}

func TestPageRegisterSubmit_SignupEnabled_InvalidEmail_FormErrorAndNoMail(t *testing.T) {
	for _, email := range []string{"", "not-an-email", "Alice <alice@test.invalid>", "a@test.invalid\r\nBcc: b@test.invalid"} {
		t.Run(email, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			enableRegistration(t, db)
			mailer := newFakeSignupMailer()

			body := serveSignup(newSignupHandler(db, mailer), http.MethodPost, "/register", url.Values{"email": {email}}).Body.String()

			if !strings.Contains(body, "Enter a valid email address") {
				t.Errorf("want the invalid-email error:\n%s", body)
			}
			mailer.assertNoneSent(t)
		})
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/handler -run 'SignupEnabled' -count=1`
Expected: build failure `h.PageRegisterComplete undefined`.

To see the Task 6 tests fail on behaviour rather than on missing symbols, create `internal/handler/signup_handler.go` with temporary stubs, which Task 7 replaces:

```go
package handler

import "net/http"

func (h *Handler) PageRegisterComplete(w http.ResponseWriter, r *http.Request)       {}
func (h *Handler) PageRegisterCompleteSubmit(w http.ResponseWriter, r *http.Request) {}
```

Re-run the command above.
Expected: FAIL; the email-only and same-response tests fail because `/register` still renders the full form.

- [ ] **Step 5: Implement** — replace the whole of `internal/handler/signup_handler.go` with:

```go
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// requestSignup answers every valid address with the same page and mails in
// the background, so neither the response nor its timing shows whether the
// address has an account.
func (h *Handler) requestSignup(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if !validEmail(email) {
		h.render(w, r, pages.RegisterEmail(view.RegisterEmailData{BasePage: basePage(r, h.Services), Email: email, Error: "Enter a valid email address"}))
		return
	}
	concurrency.Go("signup.request", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := h.Services.Signup.Request(ctx, email); err != nil {
			slog.Error("signup: request failed", "error", err)
		}
	})
	h.render(w, r, pages.RegisterCheckInbox(view.RegisterCheckInboxData{BasePage: basePage(r, h.Services)}))
}

// validEmail accepts a bare address only; refusing display names and CR/LF
// also keeps it safe to put in the To header.
func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s
}

func (h *Handler) PageRegisterComplete(w http.ResponseWriter, r *http.Request)       {}
func (h *Handler) PageRegisterCompleteSubmit(w http.ResponseWriter, r *http.Request) {}
```

Then in `internal/handler/register_handler.go`:
- In `PageRegister`, replace the final `h.render(...)` line with:

```go
	if h.Services.Signup.Enabled() {
		h.render(w, r, pages.RegisterEmail(view.RegisterEmailData{BasePage: basePage(r, h.Services)}))
		return
	}
	h.render(w, r, pages.Register(view.RegisterData{BasePage: basePage(r, h.Services)}))
```

- In `PageRegisterSubmit`, directly after the `AllowRegistration` check's closing `}`, insert:

```go
	if h.Services.Signup.Enabled() {
		h.requestSignup(w, r)
		return
	}
```

- [ ] **Step 6: Run the tests**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/handler -count=1`
Expected: `ok`. The new tests pass, and the existing `TestPageRegisterSubmit_*` tests still pass on the full form.

- [ ] **Step 7: Commit**

```bash
git add internal/handler/signup_handler.go internal/handler/register_handler.go internal/handler/auth_handler_test.go internal/handler/register_handler_test.go internal/handler/signup_handler_test.go
git commit -m "feat(signup): ask only for an email on /register when SMTP is configured" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Completion page and routes

**Files:**
- Modify: `internal/handler/signup_handler.go` (replace the two stubs; add `registrationOpen`, `usableSignup`, `renderInvalidSignupLink`, `setAuthCookie`)
- Modify: `internal/router/router.go` (two routes)
- Test: `internal/handler/signup_handler_test.go` (append)

**Interfaces:**
- Consumes:
  - Task 6 test helpers
  - `Services.Signup.GetUsable` / `Complete`, `service.ErrSignupTokenUnusable` (Task 4)
  - `pages.RegisterComplete`, `view.RegisterCompleteData` (Task 5)
  - existing `createAccountErrorMessage`, `logCreateAccountFailure`, `Services.User.GenerateTokenForUser(ctx, userID int64) (string, error)`, `middleware.RateLimit`, `accountCreationLimit`, `accountCreationWindow`
- Produces:
  - `(*Handler).PageRegisterComplete`
  - `(*Handler).PageRegisterCompleteSubmit`
  - routes `GET /register/complete/{token}` and `POST /register/complete/{token}` (rate limited)

- [ ] **Step 1: Write the failing tests** — append to `internal/handler/signup_handler_test.go`. Add `"context"`, `"fmt"` and `"sync"` to its imports.

```go
func completeForm(username string) url.Values {
	return url.Values{"username": {username}, "password": {"password123"}}
}

func TestPageRegisterComplete_UsableLink_ShowsFormWithEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	body := serveSignup(h, http.MethodGet, "/register/complete/"+token, nil).Body.String()

	if !strings.Contains(body, `value="`+email+`"`) || !strings.Contains(body, `name="username"`) {
		t.Errorf("want the completion form for %s:\n%s", email, body)
	}
}

func assertInvalidSignupLinkPage(t *testing.T, body, hiddenEmail string) {
	t.Helper()
	if !strings.Contains(body, "This link is no longer valid") {
		t.Errorf("want the invalid-link page:\n%s", body)
	}
	if strings.Contains(body, `name="username"`) {
		t.Error("an unusable link must not render the form")
	}
	if hiddenEmail != "" && strings.Contains(strings.ToLower(body), strings.ToLower(hiddenEmail)) {
		t.Errorf("an unusable link must not show its email:\n%s", body)
	}
}

func TestPageRegisterComplete_UnusableLink_GenericPage(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(t *testing.T, db *sql.DB, suffix, email string)
	}{
		{"used", func(t *testing.T, db *sql.DB, _, email string) {
			testutil.Exec(t, db, `UPDATE signup_tokens SET used_at = NOW() WHERE lower(email) = lower($1)`, email)
		}},
		{"expired", func(t *testing.T, db *sql.DB, _, email string) {
			testutil.Exec(t, db, `UPDATE signup_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE lower(email) = lower($1)`, email)
		}},
		{"email registered since", func(t *testing.T, db *sql.DB, suffix, _ string) {
			testutil.SeedUser(t, db, suffix)
		}},
	}
	for _, tc := range cases {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				db := testutil.OpenTestDB(t)
				suffix := testutil.UniqueSuffix(t)
				email := "testuser_" + suffix + "@test.invalid"
				cleanupSignup(t, db, email)
				mailer := newFakeSignupMailer()
				h := newSignupHandler(db, mailer)
				token := requestSignupLink(t, db, h, mailer, email)
				tc.spoil(t, db, suffix, email)

				var form url.Values
				if method == http.MethodPost {
					form = completeForm("signup_" + suffix)
				}
				body := serveSignup(h, method, "/register/complete/"+token, form).Body.String()

				assertInvalidSignupLinkPage(t, body, email)
			})
		}
	}
}

func TestPageRegisterComplete_UnknownToken_GenericPage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	enableRegistration(t, db)

	body := serveSignup(newSignupHandler(db, newFakeSignupMailer()), http.MethodGet, "/register/complete/nope"+testutil.UniqueSuffix(t), nil).Body.String()

	assertInvalidSignupLinkPage(t, body, "")
}

func TestPageRegisterCompleteSubmit_CreatesAccountAndSignsIn(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	rr := serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm("signup_"+suffix))

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("want 303 to /, got %d %q: %s", rr.Code, rr.Header().Get("Location"), rr.Body)
	}
	var signedIn bool
	for _, c := range rr.Result().Cookies() {
		signedIn = signedIn || (c.Name == testCookieName && c.Value != "")
	}
	if !signedIn {
		t.Error("completing signup must sign the user in")
	}
	var invited bool
	if err := db.QueryRowContext(context.Background(), `SELECT is_invited FROM users WHERE username = $1 AND email = $2`, "signup_"+suffix, email).Scan(&invited); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if invited {
		t.Error("a self-registered account must not be marked invited")
	}

	replay := serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm("signup2_"+suffix))
	assertInvalidSignupLinkPage(t, replay.Body.String(), email)
}

func TestPageRegisterCompleteSubmit_UsernameTaken_LinkStaysUsable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	body := serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm("testuser_"+suffix)).Body.String()

	assertNoDBErrorText(t, body)
	if !strings.Contains(body, "username is already taken") {
		t.Errorf("want the username-taken message:\n%s", body)
	}
	if rr := serveSignup(h, http.MethodGet, "/register/complete/"+token, nil); !strings.Contains(rr.Body.String(), `name="username"`) {
		t.Error("a failed create must leave the link usable")
	}
}

func TestPageRegisterCompleteSubmit_ConcurrentSubmits_OneAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	const submits = 5
	results := make(chan *httptest.ResponseRecorder, submits)
	var wg sync.WaitGroup
	for i := range submits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm(fmt.Sprintf("signup_%s_%d", suffix, i)))
		}()
	}
	wg.Wait()
	close(results)

	created := 0
	for rr := range results {
		if rr.Code == http.StatusSeeOther {
			created++
			continue
		}
		assertInvalidSignupLinkPage(t, rr.Body.String(), "")
	}
	if created != 1 {
		t.Errorf("want exactly 1 account, got %d", created)
	}
}

func TestPageRegisterComplete_RegistrationClosed_RedirectsToLogin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)
	setAllowRegistration(t, db, "false")

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rr := serveSignup(newSignupHandler(db, mailer), method, "/register/complete/"+token, completeForm("signup_x"))
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
			t.Errorf("%s: want 303 to /login, got %d %q", method, rr.Code, rr.Header().Get("Location"))
		}
	}
}
```

The last test builds a fresh handler after changing the setting because `SiteSettingService` caches settings per instance.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/handler -run 'PageRegisterComplete' -count=1`
Expected: FAIL. The stubs render nothing, so the form, invalid-page, redirect and sign-in assertions fail.

- [ ] **Step 3: Implement** — in `internal/handler/signup_handler.go`, delete the two stubs and add the following. Also add `"errors"`, `"github.com/go-chi/chi/v5"`, `".../internal/model"` and `".../internal/service"` to its imports.

```go
func (h *Handler) PageRegisterComplete(w http.ResponseWriter, r *http.Request) {
	if !h.registrationOpen(w, r) {
		return
	}
	signup, ok := h.usableSignup(w, r)
	if !ok {
		return
	}
	h.render(w, r, pages.RegisterComplete(view.RegisterCompleteData{BasePage: basePage(r, h.Services), Signup: signup}))
}

func (h *Handler) PageRegisterCompleteSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.registrationOpen(w, r) {
		return
	}
	signup, ok := h.usableSignup(w, r)
	if !ok {
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	renderError := func(msg string) {
		h.render(w, r, pages.RegisterComplete(view.RegisterCompleteData{BasePage: basePage(r, h.Services), Signup: signup, Username: username, Error: msg}))
	}
	if username == "" || password == "" {
		renderError("All fields are required")
		return
	}
	if len(password) < 8 {
		renderError("Password must be at least 8 characters")
		return
	}

	user, err := h.Services.Signup.Complete(r.Context(), chi.URLParam(r, "token"), username, password)
	if errors.Is(err, service.ErrSignupTokenUnusable) {
		h.renderInvalidSignupLink(w, r)
		return
	}
	if err != nil {
		logCreateAccountFailure(r.Context(), "signup: create user failed", err)
		renderError(createAccountErrorMessage(err))
		return
	}

	token, err := h.Services.User.GenerateTokenForUser(r.Context(), user.ID)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.setAuthCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// registrationOpen writes the redirect itself when it returns false. It also
// gates outstanding links, so closing registration stops them too.
func (h *Handler) registrationOpen(w http.ResponseWriter, r *http.Request) bool {
	if !h.Services.SiteSetting.AllowRegistration(r.Context()) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return false
	}
	return true
}

// usableSignup writes the response itself when it returns false.
func (h *Handler) usableSignup(w http.ResponseWriter, r *http.Request) (*model.SignupToken, bool) {
	signup, err := h.Services.Signup.GetUsable(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, service.ErrSignupTokenUnusable) {
		h.renderInvalidSignupLink(w, r)
		return nil, false
	}
	if err != nil {
		slog.Error("signup: link lookup failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	return signup, true
}

func (h *Handler) renderInvalidSignupLink(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.RegisterComplete(view.RegisterCompleteData{BasePage: basePage(r, h.Services)}))
}

func (h *Handler) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})
}
```

- [ ] **Step 4: Add the routes** — in `internal/router/router.go`, directly below the `POST /register` route line:

```go
	r.Get("/register/complete/{token}", h.PageRegisterComplete)
	r.With(middleware.RateLimit(accountCreationLimit, accountCreationWindow)).Post("/register/complete/{token}", h.PageRegisterCompleteSubmit)
```

- [ ] **Step 5: Run the tests, build and lint**

Run: `go build ./... && TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./internal/handler -count=1`
Expected: `ok`

Run: `golangci-lint run ./...`
Expected: `0 issues.`

- [ ] **Step 6: Commit**

```bash
git add internal/handler/signup_handler.go internal/handler/signup_handler_test.go internal/router/router.go
git commit -m "feat(signup): finish signup from the emailed link" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Docs and end-to-end verification

**Files:**
- Modify: `docs/access-control.md` (new "Self-Service Signup" section after "Invitation System")
- Modify: `docs/configuration.md` (the `smtp.host` row's description)
- Create (not committed): `$SP/smtp_sink.py`

`$SP` below stands for the session scratchpad, `/private/tmp/claude-501/-Users-mk-Downloads-app-Cloudzilla-cloudzilla-app--claude-worktrees-trusting-babbage-6cce94/a0ceb4d3-cb93-4ebf-a7e6-0e6329155392/scratchpad`. Type it out literally, because the Bash guard rejects variables in some forms.

**Interfaces:**
- Consumes: everything above.
- Produces: documentation, plus a verified end-to-end run.

- [ ] **Step 1: Document** — in `docs/access-control.md`, insert before the `---` that follows the Invitation System section:

```markdown
## Self-Service Signup

With `smtp.host` set, `/register` asks only for an email and always answers "Check your inbox", mailing in the background:

- New address: a single-use link to `/register/complete/{token}` (24 hours) where the owner picks a username and password.
- Address with an account: a "you already have an account — sign in" email.

The response never reveals whether an address has an account. Links are stored as SHA-256 hashes in `signup_tokens` (one per address; a new request replaces it), and an address gets at most one email per 5 minutes. `POST /register` and `POST /register/complete/{token}` are also limited to 10 per client IP per 15 minutes. Closing `allow_registration` stops outstanding links.

Without SMTP, `/register` is the classic username/email/password form, which still reveals whether an email is registered.
```

In `docs/configuration.md`, append ` Also enables email-verified signup.` to the description cell of the `smtp.host` row, keeping the table aligned.

- [ ] **Step 2: Full suite and lint**

Run: `TEST_DATABASE_DSN=postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable go test ./... -count=1`
Expected: every package `ok`.

Run: `golangci-lint run ./...`
Expected: `0 issues.`

- [ ] **Step 3: End-to-end with a local SMTP sink** — write `$SP/smtp_sink.py` with the Write tool. `EmailService` always sends `PLAIN` auth, so the sink must advertise and accept `AUTH`.

```python
import socketserver
import sys


class Sink(socketserver.StreamRequestHandler):
    def handle(self):
        def reply(s):
            self.wfile.write((s + "\r\n").encode())

        reply("220 sink")
        in_data, lines = False, []
        for raw in self.rfile:
            line = raw.decode(errors="replace").rstrip("\r\n")
            if in_data:
                if line == ".":
                    with open(sys.argv[2], "a") as f:
                        f.write("\n".join(lines) + "\n=====\n")
                    in_data, lines = False, []
                    reply("250 ok")
                else:
                    lines.append(line)
                continue
            cmd = line[:4].upper()
            if cmd == "EHLO":
                reply("250-sink")
                reply("250 AUTH PLAIN")
            elif cmd == "AUTH":
                reply("235 ok")
            elif cmd == "DATA":
                in_data = True
                reply("354 go")
            elif cmd == "QUIT":
                reply("221 bye")
                return
            else:
                reply("250 ok")


socketserver.ThreadingTCPServer.allow_reuse_address = True
socketserver.ThreadingTCPServer(("127.0.0.1", int(sys.argv[1])), Sink).serve_forever()
```

Then, one command per Bash call:

1. Start the sink (Bash `run_in_background: true`):
   `python3 $SP/smtp_sink.py 12525 $SP/mail.log`
2. Build the server:
   `go build -o $SP/czserver ./cmd/server`
   Build CSS so the pages render styled:
   `../../../bin/tailwindcss -c tailwind/tailwind.config.js -i tailwind/input.css -o cmd/server/frontend/static/main.css --minify`
3. Start the server (Bash `run_in_background: true`):
   `CZ_DATABASE_DSN="postgres://cloudzilla:test@localhost:5441/cloudzilla_test?sslmode=disable" CZ_SERVER_PORT=18080 CZ_SERVER_BASE_URL=http://localhost:18080 CZ_GIT_SSH_PORT=12222 CZ_GIT_REPOS_ROOT=$SP/run/repos CZ_GIT_SSH_HOST_KEY=$SP/run/hostkey CZ_SMTP_HOST=127.0.0.1 CZ_SMTP_PORT=12525 $SP/czserver`
4. Request a link for a fresh address:
   `curl -4 -s -o $SP/r1.html -H "X-CSRF-Token: t" -b "csrf_token=t" -d "email=e2e.signup@test.invalid" http://127.0.0.1:18080/register`
5. Read the link:
   `grep -o "http://localhost:18080/register/complete/[0-9a-f]*" $SP/mail.log`
   Expected: one URL. Call its last path segment `TOKEN` below.
6. Check the completion form:
   `curl -4 -s http://127.0.0.1:18080/register/complete/TOKEN`
   Expected: contains `value="e2e.signup@test.invalid"` and `readonly`.
7. Complete the signup:
   `curl -4 -s -i -H "X-CSRF-Token: t" -b "csrf_token=t" -d "username=e2e_signup&password=password123" http://127.0.0.1:18080/register/complete/TOKEN`
   Expected: `HTTP/1.1 303`, `Location: /`, and a `Set-Cookie: cz_token=` header.
8. Replay the link with the command from step 7.
   Expected: the body contains "This link is no longer valid".
9. Move past the 5-minute throttle so the existing address gets mail again:
   `docker exec trusting-babbage-test-db psql -U cloudzilla -d cloudzilla_test -c "UPDATE signup_tokens SET created_at = NOW() - INTERVAL '6 minutes' WHERE lower(email) = 'e2e.signup@test.invalid'"`
10. Request again with the now-registered address in another case:
    `curl -4 -s -o $SP/r2.html -H "X-CSRF-Token: t" -b "csrf_token=t" -d "email=E2E.Signup@Test.Invalid" http://127.0.0.1:18080/register`
11. Compare the two responses:
    `cmp $SP/r1.html $SP/r2.html`
    Expected: no output (identical).
12. `grep "^Subject:" $SP/mail.log`
    Expected: `Subject: Finish creating your Cloudzilla account` and then `Subject: You already have a Cloudzilla account`.
13. Stop both processes:
    `pkill -f scratchpad/czserver`
    `pkill -f smtp_sink.py`
14. Clean up the test DB only:
    `docker exec trusting-babbage-test-db psql -U cloudzilla -d cloudzilla_test -c "DELETE FROM signup_tokens WHERE lower(email) = 'e2e.signup@test.invalid'; DELETE FROM users WHERE username = 'e2e_signup'"`

- [ ] **Step 4: Commit the docs**

```bash
git add docs/access-control.md docs/configuration.md
git commit -m "docs: describe email-verified signup" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 5: Mark the spec implemented** — in `docs/superpowers/specs/2026-09-26-email-verified-signup-design.md`, change `**Status:** Design approved; not yet implemented.` to `**Status:** Implemented.` Then commit:

```bash
git add docs/superpowers/specs/2026-09-26-email-verified-signup-design.md
git commit -m "docs: mark email-verified signup spec implemented" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

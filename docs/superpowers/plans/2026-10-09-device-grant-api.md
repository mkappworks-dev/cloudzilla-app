# Device Grant API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The server side of `cz` device-code login: the `device_grants` table, its service, and `POST /api/auth/device/code` and `/token`, so a grant approved elsewhere (07b) is redeemed for a normal `czp_` PAT.

**Architecture:** `device_grants` holds pending grants only (device code stored as SHA-256). Approval is a conditional `UPDATE`; the first poll after approval runs `approved → consumed` and inserts the `access_tokens` row in one transaction (`DeviceGrantStore.Redeem` takes a callback that runs inside its transaction). Poll pacing lives in the database (`interval_secs`, `last_polled_at`). Stores → service → handler, as in `CLAUDE.md`.

**Tech Stack:** Go, chi, `database/sql` + pgx, PostgreSQL.

**Spec:** `.scratch/cz-cli/issues/07-device-code-login.md` (Design section) and `.scratch/cz-cli/issues/07a-device-grant-api.md` on branch `feat/cz-device-login` (PR open; not yet on `main`).

## Global Constraints

- Grant lifetime 900 s; initial `interval` 5 s; `slow_down` adds 5 s and is persisted.
- `user_code`: 8 characters from `BCDFGHJKLMNPQRSTVWXZ`, shown `XXXX-XXXX`, stored without the hyphen.
- `device_code`: 32 random bytes, hex; only its SHA-256 (hex) is stored.
- Scopes: default `repo:write`; each must satisfy `model.IsKnownScope` (this excludes `repo:admin`); error `invalid_scope`.
- Issued token: `czp_` via the existing mint path, hash-only, no expiry, named `cz (<hostname>) · <YYYY-MM-DD>` (or `cz CLI · <date>` without a hostname); `device_name` is stripped of control and format characters, trimmed, capped at 40 runes.
- Limits: 20 code requests per hour per IP (`middleware.RateLimit`); at most 5 live (pending or approved, unexpired) grants per IP. Polling relies on the DB interval plus the existing global anonymous `core` limiter (1000/h per IP); no extra poll middleware.
- Every response of both endpoints carries `Cache-Control: no-store`. Errors are `{"error": "<code>"}`. Both paths are exempt from CSRF; neither goes in `middleware/setup.go` or `scope.go`.
- Migration number 111 (next free on `origin/main` at planning time; re-check before committing).
- Comments only for a non-obvious why. Integration tests need `TEST_DATABASE_DSN` (a standalone Postgres, migrated; see `make test-integration`).

## File Structure

- Create `internal/db/migrations/111_device_grants.sql`
- Create `internal/model/device_grant.go`
- Create `internal/store/device_grant_store.go`, `internal/store/device_grant_store_test.go`
- Modify `internal/store/access_token_store.go` (add `CreateTx`), `internal/store/stores.go`
- Modify `internal/service/access_token_service.go` (extract `newToken`), `internal/service/services.go`
- Create `internal/service/device_grant_service.go`, `internal/service/device_grant_service_test.go`
- Create `internal/handler/device_auth_handler.go`
- Modify `internal/router/router.go`, `internal/middleware/csrf.go`, `internal/middleware/csrf_test.go`
- Create `internal/router/device_auth_test.go`
- Modify `docs/api-reference.md`

---

### Task 1: Migration, model and stores

**Files:**
- Create: `internal/db/migrations/111_device_grants.sql`, `internal/model/device_grant.go`, `internal/store/device_grant_store.go`, `internal/store/device_grant_store_test.go`
- Modify: `internal/store/access_token_store.go`, `internal/store/stores.go`

**Interfaces:**
- Produces (`store`):
  - `var ErrTooManyGrants = errors.New("too many live device grants")`
  - `func NewDeviceGrantStore(db *sql.DB) *DeviceGrantStore`
  - `Create(ctx, g *model.DeviceGrant, maxLive int, now time.Time) error` — fills `ID`, `CreatedAt`; `ErrTooManyGrants` over the cap; a duplicate `user_code` returns the raw pg unique violation
  - `GetByDeviceHash(ctx, hash string) (*model.DeviceGrant, error)` — `sql.ErrNoRows` if none
  - `GetPendingByUserCode(ctx, userCode string, now time.Time) (*model.DeviceGrant, error)`
  - `Approve(ctx, userCode string, userID int64, scopes []string, now time.Time) error` and `Deny(ctx, userCode string, userID int64, now time.Time) error` — `sql.ErrNoRows` unless the grant was pending and unexpired
  - `Touch(ctx, id int64, now time.Time) (tooFast bool, err error)`
  - `Redeem(ctx, deviceHash string, now time.Time, insert func(ctx context.Context, tx *sql.Tx, g *model.DeviceGrant) error) error` — `sql.ErrNoRows` unless the grant was approved and unexpired
  - `DeleteStale(ctx, before time.Time) error`
  - `(*AccessTokenStore).CreateTx(ctx, tx *sql.Tx, t *model.AccessToken) error`
- Produces (`model`): `DeviceGrant`, constants `DeviceGrantPending|Approved|Denied|Consumed`.

- [ ] **Step 1: Write the migration**

```sql
CREATE TABLE device_grants (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    device_code_hash TEXT NOT NULL UNIQUE,
    user_code        TEXT NOT NULL UNIQUE,
    scopes           TEXT NOT NULL,
    device_name      TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
    user_id          BIGINT REFERENCES users(id) ON DELETE CASCADE,
    requester_ip     TEXT NOT NULL,
    interval_secs    INT NOT NULL DEFAULT 5,
    last_polled_at   TIMESTAMPTZ,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_device_grants_live_ip ON device_grants(requester_ip) WHERE status IN ('pending', 'approved');
CREATE INDEX idx_device_grants_created ON device_grants(created_at);
```

- [ ] **Step 2: Write the model**

```go
package model

import (
	"database/sql"
	"time"
)

const (
	DeviceGrantPending  = "pending"
	DeviceGrantApproved = "approved"
	DeviceGrantDenied   = "denied"
	DeviceGrantConsumed = "consumed"
)

// DeviceGrant is a pending `cz auth login`: the device code's hash, the short code the
// user types in the browser, and what the user decided.
type DeviceGrant struct {
	ID             int64
	DeviceCodeHash string
	UserCode       string
	Scopes         []string
	DeviceName     string
	Status         string
	UserID         sql.NullInt64
	RequesterIP    string
	IntervalSecs   int
	LastPolledAt   *time.Time
	ExpiresAt      time.Time
	CreatedAt      time.Time
}
```

- [ ] **Step 3: Write the failing store tests** (`internal/store/device_grant_store_test.go`, package `store_test`)

```go
package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newGrant(t *testing.T, db *sql.DB, now time.Time) *model.DeviceGrant {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	g := &model.DeviceGrant{
		DeviceCodeHash: "dh_" + suffix, UserCode: "UC" + suffix, Scopes: []string{"repo:write"},
		DeviceName: "laptop", RequesterIP: "ip_" + suffix, IntervalSecs: 5, ExpiresAt: now.Add(15 * time.Minute),
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, g.RequesterIP) })
	return g
}

func TestDeviceGrantStore_CreateCapsLiveGrantsPerIP(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	first := newGrant(t, db, now)
	for i := 0; i < 2; i++ {
		g := *first
		g.DeviceCodeHash, g.UserCode = first.DeviceCodeHash+string(rune('a'+i)), first.UserCode+string(rune('a'+i))
		if err := s.Create(ctx, &g, 2, now); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	g := *first
	g.DeviceCodeHash, g.UserCode = first.DeviceCodeHash+"z", first.UserCode+"z"
	if err := s.Create(ctx, &g, 2, now); !errors.Is(err, store.ErrTooManyGrants) {
		t.Fatalf("third Create = %v; want ErrTooManyGrants", err)
	}
}

func TestDeviceGrantStore_ApproveOnlyWhilePending(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, []string{"repo:read"}, now); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, []string{"repo:read"}, now); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("second Approve = %v; want sql.ErrNoRows", err)
	}
	if err := s.Deny(ctx, g.UserCode, uid, now); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Deny after Approve = %v; want sql.ErrNoRows", err)
	}
	got, err := s.GetByDeviceHash(ctx, g.DeviceCodeHash)
	if err != nil || got.Status != model.DeviceGrantApproved || len(got.Scopes) != 1 || got.Scopes[0] != "repo:read" {
		t.Errorf("grant = %+v, %v; want approved with narrowed scopes", got, err)
	}
}

func TestDeviceGrantStore_ApproveRefusesExpired(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, g.Scopes, now.Add(16*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Approve after expiry = %v; want sql.ErrNoRows", err)
	}
}

func TestDeviceGrantStore_TouchFlagsEarlyPollsAndGrowsInterval(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	if fast, err := s.Touch(ctx, g.ID, now); err != nil || fast {
		t.Fatalf("first Touch = %v, %v; want false", fast, err)
	}
	if fast, _ := s.Touch(ctx, g.ID, now.Add(2*time.Second)); !fast {
		t.Error("poll after 2s with a 5s interval should be too fast")
	}
	got, _ := s.GetByDeviceHash(ctx, g.DeviceCodeHash)
	if got.IntervalSecs != 10 {
		t.Errorf("interval = %d; want 10 after slow_down", got.IntervalSecs)
	}
	if fast, _ := s.Touch(ctx, g.ID, now.Add(13*time.Second)); fast {
		t.Error("poll 11s after the last one with a 10s interval should be fine")
	}
}

func TestDeviceGrantStore_RedeemOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	noop := func(context.Context, *sql.Tx, *model.DeviceGrant) error { return nil }
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, noop); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Redeem of a pending grant = %v; want sql.ErrNoRows", err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, g.Scopes, now); err != nil {
		t.Fatal(err)
	}
	var seen *model.DeviceGrant
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, func(_ context.Context, _ *sql.Tx, got *model.DeviceGrant) error { seen = got; return nil }); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if seen == nil || seen.UserID.Int64 != uid {
		t.Errorf("callback grant = %+v; want user %d", seen, uid)
	}
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, noop); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("second Redeem = %v; want sql.ErrNoRows", err)
	}
}

func TestDeviceGrantStore_RedeemRollsBackWhenInsertFails(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	_ = s.Create(ctx, g, 5, now)
	_ = s.Approve(ctx, g.UserCode, uid, g.Scopes, now)
	boom := errors.New("boom")
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, func(context.Context, *sql.Tx, *model.DeviceGrant) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Redeem = %v; want boom", err)
	}
	got, _ := s.GetByDeviceHash(ctx, g.DeviceCodeHash)
	if got.Status != model.DeviceGrantApproved {
		t.Errorf("status = %s; want approved so the next poll can retry", got.Status)
	}
}
```

- [ ] **Step 4: Run to verify they fail**

Run: `go test ./internal/store -run DeviceGrant -v`
Expected: compile failure (`store.NewDeviceGrantStore` undefined); with `TEST_DATABASE_DSN` unset the tests would skip, so export it first (a migrated standalone Postgres).

- [ ] **Step 5: Implement `CreateTx` on `AccessTokenStore`**

Replace the body of `Create` in `internal/store/access_token_store.go` so both entry points share one insert:

```go
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *AccessTokenStore) Create(ctx context.Context, t *model.AccessToken) error {
	return s.insert(ctx, s.db, t)
}

// CreateTx inserts t inside tx, for callers that must commit it with other changes.
func (s *AccessTokenStore) CreateTx(ctx context.Context, tx *sql.Tx, t *model.AccessToken) error {
	return s.insert(ctx, tx, t)
}

func (s *AccessTokenStore) insert(ctx context.Context, q queryRower, t *model.AccessToken) error {
	t.ScopesRaw = strings.Join(t.Scopes, ",")
	t.TargetsRaw = strings.Join(t.Targets, ",")
	err := q.QueryRowContext(ctx,
		`INSERT INTO access_tokens (user_id, name, token_hash, last_eight, scopes, expires_at, signing_key, targets)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		t.UserID, t.Name, t.TokenHash, t.LastEight, t.ScopesRaw, t.ExpiresAt, t.SigningKey, t.TargetsRaw,
	).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("access token create: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Implement `DeviceGrantStore`** (`internal/store/device_grant_store.go`)

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var ErrTooManyGrants = errors.New("too many live device grants")

// DeviceGrantStore provides database operations for device-code login grants.
type DeviceGrantStore struct{ db *sql.DB }

func NewDeviceGrantStore(db *sql.DB) *DeviceGrantStore { return &DeviceGrantStore{db: db} }

const deviceGrantColumns = `id, device_code_hash, user_code, scopes, device_name, status, user_id, requester_ip, interval_secs, last_polled_at, expires_at, created_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanDeviceGrant(row rowScanner) (*model.DeviceGrant, error) {
	g := &model.DeviceGrant{}
	var scopes string
	if err := row.Scan(&g.ID, &g.DeviceCodeHash, &g.UserCode, &scopes, &g.DeviceName, &g.Status, &g.UserID,
		&g.RequesterIP, &g.IntervalSecs, &g.LastPolledAt, &g.ExpiresAt, &g.CreatedAt); err != nil {
		return nil, err
	}
	if scopes != "" {
		g.Scopes = strings.Split(scopes, ",")
	}
	return g, nil
}

// Create inserts g unless requester_ip already holds maxLive unexpired grants. The count and
// the insert are one statement, but two concurrent requests can both pass it; the per-hour
// request limiter on the route bounds that overshoot.
func (s *DeviceGrantStore) Create(ctx context.Context, g *model.DeviceGrant, maxLive int, now time.Time) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO device_grants (device_code_hash, user_code, scopes, device_name, requester_ip, interval_secs, expires_at)
		 SELECT $1, $2, $3, $4, $5, $6, $7
		 WHERE (SELECT COUNT(*) FROM device_grants
		        WHERE requester_ip = $5 AND status IN ('pending', 'approved') AND expires_at > $8) < $9
		 RETURNING id, created_at`,
		g.DeviceCodeHash, g.UserCode, strings.Join(g.Scopes, ","), g.DeviceName, g.RequesterIP, g.IntervalSecs, g.ExpiresAt, now, maxLive,
	).Scan(&g.ID, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTooManyGrants
	}
	if err != nil {
		return fmt.Errorf("device grant create: %w", err)
	}
	return nil
}

func (s *DeviceGrantStore) GetByDeviceHash(ctx context.Context, hash string) (*model.DeviceGrant, error) {
	return scanDeviceGrant(s.db.QueryRowContext(ctx,
		`SELECT `+deviceGrantColumns+` FROM device_grants WHERE device_code_hash = $1`, hash))
}

func (s *DeviceGrantStore) GetPendingByUserCode(ctx context.Context, userCode string, now time.Time) (*model.DeviceGrant, error) {
	return scanDeviceGrant(s.db.QueryRowContext(ctx,
		`SELECT `+deviceGrantColumns+` FROM device_grants WHERE user_code = $1 AND status = 'pending' AND expires_at > $2`, userCode, now))
}

// Approve records userID's decision and the scopes they kept. Only a pending, unexpired
// grant changes, so a second click or a late one is sql.ErrNoRows.
func (s *DeviceGrantStore) Approve(ctx context.Context, userCode string, userID int64, scopes []string, now time.Time) error {
	return s.decide(ctx, userCode, userID, model.DeviceGrantApproved, strings.Join(scopes, ","), now)
}

func (s *DeviceGrantStore) Deny(ctx context.Context, userCode string, userID int64, now time.Time) error {
	return s.decide(ctx, userCode, userID, model.DeviceGrantDenied, "", now)
}

func (s *DeviceGrantStore) decide(ctx context.Context, userCode string, userID int64, status, scopes string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE device_grants SET status = $1, user_id = $2, scopes = CASE WHEN $3 = '' THEN scopes ELSE $3 END
		 WHERE user_code = $4 AND status = 'pending' AND expires_at > $5`,
		status, userID, scopes, userCode, now)
	if err != nil {
		return fmt.Errorf("device grant decide: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Touch records a poll and reports whether it came before last_polled_at plus the interval.
// An early poll also lengthens the interval by 5 s, as RFC 8628 §3.5 asks of slow_down.
func (s *DeviceGrantStore) Touch(ctx context.Context, id int64, now time.Time) (bool, error) {
	var tooFast bool
	err := s.db.QueryRowContext(ctx,
		`WITH prev AS (
		     SELECT id, interval_secs,
		            (last_polled_at IS NOT NULL AND $2::timestamptz < last_polled_at + make_interval(secs => interval_secs::double precision)) AS fast
		     FROM device_grants WHERE id = $1 FOR UPDATE
		 )
		 UPDATE device_grants g
		 SET last_polled_at = $2, interval_secs = prev.interval_secs + CASE WHEN prev.fast THEN 5 ELSE 0 END
		 FROM prev WHERE g.id = prev.id
		 RETURNING prev.fast`, id, now).Scan(&tooFast)
	if err != nil {
		return false, fmt.Errorf("device grant touch: %w", err)
	}
	return tooFast, nil
}

// Redeem claims an approved, unexpired grant for good and runs insert in the same
// transaction, so a token exists exactly when the grant is consumed. A concurrent second
// Redeem waits on the row lock, then finds the grant consumed and gets sql.ErrNoRows.
func (s *DeviceGrantStore) Redeem(ctx context.Context, deviceHash string, now time.Time,
	insert func(ctx context.Context, tx *sql.Tx, g *model.DeviceGrant) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("device grant redeem: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	g, err := scanDeviceGrant(tx.QueryRowContext(ctx,
		`UPDATE device_grants SET status = 'consumed'
		 WHERE device_code_hash = $1 AND status = 'approved' AND expires_at > $2
		 RETURNING `+deviceGrantColumns, deviceHash, now))
	if err != nil {
		return err
	}
	if err := insert(ctx, tx, g); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DeviceGrantStore) DeleteStale(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM device_grants WHERE created_at < $1`, before)
	return err
}
```

- [ ] **Step 7: Wire into `Stores`**

In `internal/store/stores.go` add field `DeviceGrant *DeviceGrantStore` next to `AccessToken` and `DeviceGrant: NewDeviceGrantStore(database),` in `New`.

- [ ] **Step 8: Run tests, then commit**

Run: `go test ./internal/store -run 'DeviceGrant|AccessToken' -v`
Expected: PASS. Then:

```bash
git add internal/db/migrations/111_device_grants.sql internal/model/device_grant.go internal/store
git commit -m "feat(auth): device_grants table and store"
```

---

### Task 2: DeviceGrantService

**Files:**
- Create: `internal/service/device_grant_service.go`, `internal/service/device_grant_service_test.go`
- Modify: `internal/service/access_token_service.go`, `internal/service/services.go`

**Interfaces:**
- Consumes: Task 1's `DeviceGrantStore` methods, `AccessTokenStore.CreateTx`.
- Produces (`service`):
  - `type DeviceCode struct { DeviceCode, UserCode string; ExpiresIn, Interval int }`
  - `type DeviceToken struct { AccessToken string; Scopes []string }`
  - `func NewDeviceGrantService(grants *store.DeviceGrantStore, tokens *store.AccessTokenStore) *DeviceGrantService`
  - `Create(ctx, ip string, scopes []string, deviceName string) (*DeviceCode, error)`
  - `Poll(ctx, deviceCode string) (*DeviceToken, error)`
  - `Lookup(ctx, userCode string) (*model.DeviceGrant, error)`, `Approve(ctx, userCode string, userID int64, scopes []string) error`, `Deny(ctx, userCode string, userID int64) error` (used by 07b)
  - Sentinels: `ErrDeviceGrantNotFound`, `ErrTooManyDeviceGrants`, `ErrAuthorizationPending`, `ErrSlowDown`, `ErrExpiredToken`, `ErrAccessDenied`, `ErrInvalidDeviceGrant`; `ErrInvalidScope` already exists.
  - `(*AccessTokenService)`'s mint step extracted as `func newToken(userID int64, nt NewToken) (raw string, t *model.AccessToken, err error)`.

- [ ] **Step 1: Extract `newToken` from `AccessTokenService.Create`**

In `internal/service/access_token_service.go`, replace the byte generation, hashing and struct building in `Create` with:

```go
// newToken builds a token record and its raw value without storing either.
func newToken(userID int64, nt NewToken) (string, *model.AccessToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token bytes: %w", err)
	}
	rawHex := "czp_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(rawHex))
	return rawHex, &model.AccessToken{
		UserID:     userID,
		Name:       nt.Name,
		TokenHash:  hex.EncodeToString(sum[:]),
		LastEight:  rawHex[len(rawHex)-8:],
		Scopes:     nt.Scopes,
		ExpiresAt:  nt.ExpiresAt,
		SigningKey: nt.SigningKey,
		Targets:    nt.Targets,
	}, nil
}

func (s *AccessTokenService) Create(ctx context.Context, userID int64, nt NewToken) (string, *model.AccessToken, error) {
	nt, err := s.Check(ctx, userID, nt)
	if err != nil {
		return "", nil, err
	}
	rawHex, t, err := newToken(userID, nt)
	if err != nil {
		return "", nil, err
	}
	if err := s.tokens.Create(ctx, t); err != nil {
		return "", nil, err
	}
	return rawHex, t, nil
}
```

Run `go test ./internal/service -run AccessToken` — Expected: PASS (behaviour unchanged).

- [ ] **Step 2: Write the failing service tests** (`internal/service/device_grant_service_test.go`, package `service_test`; mirror the setup of `access_token_service_test.go`)

```go
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newDeviceSvc(t *testing.T) (*service.DeviceGrantService, *service.AccessTokenService, int64, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	stores := store.New(db)
	suffix := testutil.UniqueSuffix(t)
	uid := testutil.SeedUser(t, db, suffix)
	ip := "ip_" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })
	return service.NewDeviceGrantService(stores.DeviceGrant, stores.AccessToken),
		service.NewAccessTokenService(stores.AccessToken, stores.User), uid, ip
}

func TestDeviceGrantService_Create(t *testing.T) {
	svc, _, _, ip := newDeviceSvc(t)
	ctx := context.Background()

	dc, err := svc.Create(ctx, ip, nil, "laptop")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dc.ExpiresIn != 900 || dc.Interval != 5 || len(dc.DeviceCode) != 64 {
		t.Errorf("DeviceCode = %+v; want expires 900, interval 5, 64-char hex", dc)
	}
	if len(dc.UserCode) != 9 || dc.UserCode[4] != '-' || strings.Trim(dc.UserCode, "BCDFGHJKLMNPQRSTVWXZ-") != "" {
		t.Errorf("UserCode = %q; want XXXX-XXXX from the consonant alphabet", dc.UserCode)
	}

	for _, bad := range [][]string{{"repo:admin"}, {"nonsense"}, {"repo:read", "repo:admin"}} {
		if _, err := svc.Create(ctx, ip, bad, ""); !errors.Is(err, service.ErrInvalidScope) {
			t.Errorf("Create(%v) = %v; want ErrInvalidScope", bad, err)
		}
	}
}

func TestDeviceGrantService_CreateCapsLiveGrants(t *testing.T) {
	svc, _, _, ip := newDeviceSvc(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := svc.Create(ctx, ip, nil, ""); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	if _, err := svc.Create(ctx, ip, nil, ""); !errors.Is(err, service.ErrTooManyDeviceGrants) {
		t.Errorf("sixth Create = %v; want ErrTooManyDeviceGrants", err)
	}
}

func TestDeviceGrantService_PollLifecycle(t *testing.T) {
	svc, tokens, uid, ip := newDeviceSvc(t)
	ctx := context.Background()
	dc, _ := svc.Create(ctx, ip, []string{"repo:write", "repo:read"}, "mk-laptop\x1b[31m")

	if _, err := svc.Poll(ctx, "nope"); !errors.Is(err, service.ErrInvalidDeviceGrant) {
		t.Errorf("unknown device code = %v; want ErrInvalidDeviceGrant", err)
	}
	if _, err := svc.Poll(ctx, dc.DeviceCode); !errors.Is(err, service.ErrAuthorizationPending) {
		t.Fatalf("first poll = %v; want ErrAuthorizationPending", err)
	}
	if _, err := svc.Poll(ctx, dc.DeviceCode); !errors.Is(err, service.ErrSlowDown) {
		t.Errorf("immediate second poll = %v; want ErrSlowDown", err)
	}

	if err := svc.Approve(ctx, dc.UserCode, uid, []string{"repo:read"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	svc.SetClock(func() time.Time { return time.Now().Add(time.Minute) })
	tok, err := svc.Poll(ctx, dc.DeviceCode)
	if err != nil {
		t.Fatalf("poll after approval: %v", err)
	}
	if !strings.HasPrefix(tok.AccessToken, "czp_") || len(tok.Scopes) != 1 || tok.Scopes[0] != "repo:read" {
		t.Errorf("token = %+v; want a czp_ token with only the narrowed scope", tok)
	}
	if _, _, err := tokens.Validate(ctx, tok.AccessToken); err != nil {
		t.Errorf("Validate: %v", err)
	}
	list, _ := tokens.List(ctx, uid)
	wantName := "cz (mk-laptop[31m) · " + time.Now().UTC().Format("2006-01-02")
	if len(list) != 1 || list[0].Name != wantName || list[0].ExpiresAt != nil {
		t.Errorf("listed tokens = %+v; want one named %q (escape byte stripped), no expiry", list, wantName)
	}
	if _, err := svc.Poll(ctx, dc.DeviceCode); !errors.Is(err, service.ErrInvalidDeviceGrant) {
		t.Errorf("poll after redeeming = %v; want ErrInvalidDeviceGrant", err)
	}
}

func TestDeviceGrantService_DeniedAndExpired(t *testing.T) {
	svc, _, uid, ip := newDeviceSvc(t)
	ctx := context.Background()

	denied, _ := svc.Create(ctx, ip, nil, "")
	if err := svc.Deny(ctx, denied.UserCode, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Poll(ctx, denied.DeviceCode); !errors.Is(err, service.ErrAccessDenied) {
		t.Errorf("denied poll = %v; want ErrAccessDenied", err)
	}
	if err := svc.Approve(ctx, denied.UserCode, uid, nil); !errors.Is(err, service.ErrDeviceGrantNotFound) {
		t.Errorf("Approve of a denied grant = %v; want ErrDeviceGrantNotFound", err)
	}

	expired, _ := svc.Create(ctx, ip, nil, "")
	svc.SetClock(func() time.Time { return time.Now().Add(16 * time.Minute) })
	if _, err := svc.Poll(ctx, expired.DeviceCode); !errors.Is(err, service.ErrExpiredToken) {
		t.Errorf("expired poll = %v; want ErrExpiredToken", err)
	}
}

func TestDeviceGrantService_ApproveCannotAddScopes(t *testing.T) {
	svc, _, uid, ip := newDeviceSvc(t)
	ctx := context.Background()
	dc, _ := svc.Create(ctx, ip, []string{"repo:read"}, "")
	for _, bad := range [][]string{{"repo:write"}, {"repo:admin"}, {}} {
		if err := svc.Approve(ctx, dc.UserCode, uid, bad); !errors.Is(err, service.ErrInvalidScope) {
			t.Errorf("Approve(%v) = %v; want ErrInvalidScope", bad, err)
		}
	}
	if err := svc.Approve(ctx, strings.ToLower(dc.UserCode), uid, []string{"repo:read"}); err != nil {
		t.Errorf("Approve with a lower-case code: %v", err)
	}
}

func TestDeviceGrantService_ConcurrentPollsMintOneToken(t *testing.T) {
	svc, tokens, uid, ip := newDeviceSvc(t)
	ctx := context.Background()
	dc, _ := svc.Create(ctx, ip, nil, "")
	_ = svc.Approve(ctx, dc.UserCode, uid, []string{"repo:write"})
	svc.SetClock(func() time.Time { return time.Now().Add(time.Hour).Add(-50 * time.Minute) })

	const n = 8
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Poll(ctx, dc.DeviceCode); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	list, _ := tokens.List(ctx, uid)
	if ok.Load() != 1 || len(list) != 1 {
		t.Errorf("%d polls succeeded and %d tokens exist; want exactly 1 of each", ok.Load(), len(list))
	}
}
```

Add `"sync"`, `"sync/atomic"`, `"time"` to the imports. The test sets the clock because a real grant is polled twice within the interval; `SetClock` is a test seam on the service (exported for the `service_test` package).

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/service -run DeviceGrant -v`
Expected: compile failure (`service.NewDeviceGrantService` undefined).

- [ ] **Step 4: Implement the service** (`internal/service/device_grant_service.go`)

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
	"log/slog"
	"math/big"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// Poll outcomes are RFC 8628 §3.5 error codes; the handler sends err.Error() as the body.
var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	ErrExpiredToken         = errors.New("expired_token")
	ErrAccessDenied         = errors.New("access_denied")
	ErrInvalidDeviceGrant   = errors.New("invalid_grant")

	ErrDeviceGrantNotFound = errors.New("device grant not found")
	ErrTooManyDeviceGrants = errors.New("too many pending device logins")
)

const (
	DeviceGrantTTL      = 15 * time.Minute
	deviceInterval      = 5
	maxLiveGrantsPerIP  = 5
	userCodeAlphabet    = "BCDFGHJKLMNPQRSTVWXZ"
	userCodeLen         = 8
	deviceNameMax       = 40
	staleGrantAge       = 24 * time.Hour
	userCodeAttempts    = 3
	defaultDeviceName   = "cz CLI"
)

// DeviceCode is what a client gets when it asks to log in.
type DeviceCode struct {
	DeviceCode string
	UserCode   string
	ExpiresIn  int
	Interval   int
}

// DeviceToken is the result of a successful poll; AccessToken is shown once.
type DeviceToken struct {
	AccessToken string
	Scopes      []string
}

// DeviceGrantService runs the device-code login: the CLI asks for a code, a signed-in
// user approves it in the browser, and the CLI's next poll turns it into a personal access token.
type DeviceGrantService struct {
	grants *store.DeviceGrantStore
	tokens *store.AccessTokenStore
	now    func() time.Time
}

func NewDeviceGrantService(grants *store.DeviceGrantStore, tokens *store.AccessTokenStore) *DeviceGrantService {
	return &DeviceGrantService{grants: grants, tokens: tokens, now: time.Now}
}

// SetClock replaces the time source, for tests.
func (s *DeviceGrantService) SetClock(now func() time.Time) { s.now = now }

// Create starts a login for the client at ip. Scopes default to repo:write; repo:admin and
// unknown scopes are ErrInvalidScope.
func (s *DeviceGrantService) Create(ctx context.Context, ip string, scopes []string, deviceName string) (*DeviceCode, error) {
	if len(scopes) == 0 {
		scopes = []string{model.ScopeRepoWrite}
	}
	scopes, err := validateScopes(scopes)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := s.grants.DeleteStale(ctx, now.Add(-staleGrantAge)); err != nil {
		slog.Warn("delete stale device grants", "error", err)
	}

	deviceCode, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	g := &model.DeviceGrant{
		DeviceCodeHash: hashDeviceCode(deviceCode),
		Scopes:         scopes,
		DeviceName:     cleanDeviceName(deviceName),
		RequesterIP:    ip,
		IntervalSecs:   deviceInterval,
		ExpiresAt:      now.Add(DeviceGrantTTL),
	}
	for attempt := 0; ; attempt++ {
		if g.UserCode, err = newUserCode(); err != nil {
			return nil, err
		}
		err = s.grants.Create(ctx, g, maxLiveGrantsPerIP, now)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && attempt < userCodeAttempts {
			continue
		}
		break
	}
	if errors.Is(err, store.ErrTooManyGrants) {
		return nil, ErrTooManyDeviceGrants
	}
	if err != nil {
		return nil, err
	}
	return &DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   g.UserCode[:4] + "-" + g.UserCode[4:],
		ExpiresIn:  int(DeviceGrantTTL.Seconds()),
		Interval:   deviceInterval,
	}, nil
}

// Poll answers the client's token request: a poll error, or the token on the first poll after approval.
func (s *DeviceGrantService) Poll(ctx context.Context, deviceCode string) (*DeviceToken, error) {
	now := s.now()
	hash := hashDeviceCode(deviceCode)
	g, err := s.grants.GetByDeviceHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) || err == nil && g.Status == model.DeviceGrantConsumed {
		return nil, ErrInvalidDeviceGrant
	}
	if err != nil {
		return nil, err
	}
	tooFast, err := s.grants.Touch(ctx, g.ID, now)
	if err != nil {
		return nil, err
	}
	switch {
	case tooFast:
		return nil, ErrSlowDown
	case !now.Before(g.ExpiresAt):
		return nil, ErrExpiredToken
	case g.Status == model.DeviceGrantPending:
		return nil, ErrAuthorizationPending
	case g.Status == model.DeviceGrantDenied:
		return nil, ErrAccessDenied
	}

	var raw string
	err = s.grants.Redeem(ctx, hash, now, func(ctx context.Context, tx *sql.Tx, g *model.DeviceGrant) error {
		var t *model.AccessToken
		var err error
		raw, t, err = newToken(g.UserID.Int64, NewToken{Name: tokenName(g.DeviceName, now), Scopes: g.Scopes})
		if err != nil {
			return err
		}
		return s.tokens.CreateTx(ctx, tx, t)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidDeviceGrant
	}
	if err != nil {
		return nil, err
	}
	return &DeviceToken{AccessToken: raw, Scopes: g.Scopes}, nil
}

// Lookup returns the pending grant for a code as the user typed it.
func (s *DeviceGrantService) Lookup(ctx context.Context, userCode string) (*model.DeviceGrant, error) {
	code, ok := normalizeUserCode(userCode)
	if !ok {
		return nil, ErrDeviceGrantNotFound
	}
	g, err := s.grants.GetPendingByUserCode(ctx, code, s.now())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceGrantNotFound
	}
	return g, err
}

// Approve lets userID release the grant with scopes, which must be a non-empty subset of
// what the client asked for: the page can narrow a request, never widen it.
func (s *DeviceGrantService) Approve(ctx context.Context, userCode string, userID int64, scopes []string) error {
	g, err := s.Lookup(ctx, userCode)
	if err != nil {
		return err
	}
	if len(scopes) == 0 || slices.ContainsFunc(scopes, func(sc string) bool { return !slices.Contains(g.Scopes, sc) }) {
		return ErrInvalidScope
	}
	return s.decide(s.grants.Approve(ctx, g.UserCode, userID, scopes, s.now()))
}

func (s *DeviceGrantService) Deny(ctx context.Context, userCode string, userID int64) error {
	g, err := s.Lookup(ctx, userCode)
	if err != nil {
		return err
	}
	return s.decide(s.grants.Deny(ctx, g.UserCode, userID, s.now()))
}

func (s *DeviceGrantService) decide(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeviceGrantNotFound
	}
	return err
}

func normalizeUserCode(in string) (string, bool) {
	code := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(in))
	if len(code) != userCodeLen || strings.Trim(code, userCodeAlphabet) != "" {
		return "", false
	}
	return code, true
}

func newUserCode() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(userCodeAlphabet)))
	for i := 0; i < userCodeLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate user code: %w", err)
		}
		b.WriteByte(userCodeAlphabet[n.Int64()])
	}
	return b.String(), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate device code: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashDeviceCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// cleanDeviceName drops control and format characters (bidi overrides could reorder the
// name on the approval page), trims, and caps the length.
func cleanDeviceName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > deviceNameMax {
		s = strings.TrimSpace(string(r[:deviceNameMax]))
	}
	return s
}

func tokenName(deviceName string, now time.Time) string {
	label := defaultDeviceName
	if deviceName != "" {
		label = "cz (" + deviceName + ")"
	}
	return label + " · " + now.UTC().Format("2006-01-02")
}
```

Check `validateScopes` (in `oauth_app_service.go`) before relying on it: it must reject `repo:admin` and unknown scopes with `ErrInvalidScope` and deduplicate. If it does not deduplicate, deduplicate in `Create`.

- [ ] **Step 5: Wire into `Services`**

In `internal/service/services.go` add field `DeviceGrant *DeviceGrantService` and `DeviceGrant: NewDeviceGrantService(stores.DeviceGrant, stores.AccessToken),` next to `AccessToken`.

- [ ] **Step 6: Run tests, then commit**

Run: `go test ./internal/service -run 'DeviceGrant|AccessToken' -race -v`
Expected: PASS.

```bash
git add internal/service
git commit -m "feat(auth): device grant service"
```

---

### Task 3: Endpoints, routes and CSRF exemption

**Files:**
- Create: `internal/handler/device_auth_handler.go`, `internal/router/device_auth_test.go`
- Modify: `internal/router/router.go`, `internal/middleware/csrf.go`, `internal/middleware/csrf_test.go`

**Interfaces:**
- Consumes: `service.DeviceGrantService` (Task 2).
- Produces: `POST /api/auth/device/code` and `POST /api/auth/device/token`.

- [ ] **Step 1: Write the failing CSRF test**

In `internal/middleware/csrf_test.go`, next to the OAuth-token case:

```go
	t.Run("device login endpoints bypass CSRF", func(t *testing.T) {
		for _, path := range []string{"/api/auth/device/code", "/api/auth/device/token"} {
			req := httptest.NewRequest("POST", path, strings.NewReader("scope=repo:read"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			csrf.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("%s returned %d, want 200", path, rec.Code)
			}
		}
	})
```

Run: `go test ./internal/middleware -run CSRF -v` — Expected: the new case FAILS (403).

- [ ] **Step 2: Exempt the paths**

In `internal/middleware/csrf.go`, replace the `/oauth/token` check:

```go
			// These endpoints are called by CLIs and backends that hold no cookie; they
			// authenticate with a client secret or a device code.
			if r.URL.Path == "/oauth/token" || r.URL.Path == "/api/auth/device/code" || r.URL.Path == "/api/auth/device/token" {
```

Run the CSRF test again — Expected: PASS.

- [ ] **Step 3: Write the failing router tests** (`internal/router/device_auth_test.go`, package `router_test`, using `newTestRouter` and `serve` from sibling tests)

```go
package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func postDevice(h http.Handler, path string, form url.Values, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = ip + ":4444"
	return serve(h, req)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return m
}

func TestDeviceLogin_EndToEnd(t *testing.T) {
	h, svc, db := newTestRouter(t)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	ip := "198.51.100." + testutil.UniqueSuffix(t)[:2]
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })

	rec := postDevice(h, "/api/auth/device/code", url.Values{"scope": {"repo:read repo:write"}, "device_name": {"mk-laptop"}}, ip)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code = %d, Cache-Control %q; want 200, no-store", rec.Code, rec.Header().Get("Cache-Control"))
	}
	body := decode(t, rec)
	if body["verification_uri"] != "http://localhost/login/device" || body["expires_in"] != float64(900) || body["interval"] != float64(5) {
		t.Errorf("code body = %v", body)
	}
	if _, has := body["verification_uri_complete"]; has {
		t.Error("the response must not carry verification_uri_complete")
	}
	deviceCode, userCode := body["device_code"].(string), body["user_code"].(string)
	poll := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {deviceCode}}

	if rec := postDevice(h, "/api/auth/device/token", poll, ip); rec.Code != 400 || decode(t, rec)["error"] != "authorization_pending" {
		t.Errorf("pending poll = %d %s", rec.Code, rec.Body)
	}
	if rec := postDevice(h, "/api/auth/device/token", poll, ip); decode(t, rec)["error"] != "slow_down" {
		t.Errorf("early poll = %s; want slow_down", rec.Body)
	}

	if err := svc.DeviceGrant.Approve(context.Background(), userCode, uid, []string{"repo:read"}); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE device_grants SET last_polled_at = $2 WHERE requester_ip = $1`, ip, time.Now().Add(-time.Hour))
	rec = postDevice(h, "/api/auth/device/token", poll, ip)
	tok := decode(t, rec)
	if rec.Code != 200 || tok["token_type"] != "bearer" || tok["scope"] != "repo:read" || !strings.HasPrefix(tok["access_token"].(string), "czp_") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token response = %d %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest("GET", "/api/user", nil)
	req.Header.Set("Authorization", "Bearer "+tok["access_token"].(string))
	if rec := serve(h, req); rec.Code != 200 {
		t.Errorf("GET /api/user with the issued token = %d; want 200", rec.Code)
	}

	if rec := postDevice(h, "/api/auth/device/token", poll, ip); rec.Code != 400 || decode(t, rec)["error"] != "invalid_grant" {
		t.Errorf("second redemption = %d %s; want invalid_grant", rec.Code, rec.Body)
	}
}

func TestDeviceLogin_Errors(t *testing.T) {
	h, _, db := newTestRouter(t)
	ip := "203.0.113." + testutil.UniqueSuffix(t)[:2]
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })

	if rec := postDevice(h, "/api/auth/device/code", url.Values{"scope": {"repo:admin"}}, ip); rec.Code != 400 || decode(t, rec)["error"] != "invalid_scope" {
		t.Errorf("repo:admin = %d %s; want 400 invalid_scope", rec.Code, rec.Body)
	}
	if rec := postDevice(h, "/api/auth/device/token", url.Values{"grant_type": {"password"}, "device_code": {"x"}}, ip); decode(t, rec)["error"] != "unsupported_grant_type" {
		t.Errorf("wrong grant_type = %s", rec.Body)
	}
	if rec := postDevice(h, "/api/auth/device/token", url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, ip); decode(t, rec)["error"] != "invalid_request" {
		t.Errorf("missing device_code = %s", rec.Body)
	}
	for i := 0; i < 5; i++ {
		postDevice(h, "/api/auth/device/code", url.Values{}, ip)
	}
	if rec := postDevice(h, "/api/auth/device/code", url.Values{}, ip); rec.Code != 429 {
		t.Errorf("sixth live grant = %d; want 429", rec.Code)
	}
}

func TestDeviceLogin_NoCSRFToken(t *testing.T) {
	h, _, db := newTestRouter(t)
	ip := "192.0.2." + testutil.UniqueSuffix(t)[:2]
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })
	if rec := postDevice(h, "/api/auth/device/code", url.Values{}, ip); rec.Code != 200 {
		t.Errorf("code without a CSRF token = %d; want 200", rec.Code)
	}
}
```

If `serve` or `testutil.UniqueSuffix` returns something shorter than two characters or `serve` lacks the signature used here, adapt the helper calls to what `login_next_test.go` defines; keep each test's IP unique so rate-limit and live-grant counters don't collide.

Run: `go test ./internal/router -run DeviceLogin -v` — Expected: FAIL (404 on the routes).

- [ ] **Step 4: Write the handler** (`internal/handler/device_auth_handler.go`)

```go
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// deviceParams reads the body of a device endpoint: JSON or a URL-encoded form. The query
// string is never read, because a device code in a URL ends up in access logs.
func deviceParams(r *http.Request) (url.Values, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var m map[string]string
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			return nil, err
		}
		v := url.Values{}
		for k, val := range m {
			v.Set(k, val)
		}
		return v, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	return r.PostForm, nil
}

// DeviceCode handles POST /api/auth/device/code.
func (h *Handler) DeviceCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, err := deviceParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	dc, err := h.Services.DeviceGrant.Create(r.Context(), middleware.RemoteIP(r), strings.Fields(p.Get("scope")), p.Get("device_name"))
	switch {
	case errors.Is(err, service.ErrInvalidScope):
		writeError(w, http.StatusBadRequest, "invalid_scope")
	case errors.Is(err, service.ErrTooManyDeviceGrants):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too_many_requests")
	case err != nil:
		slog.Error("device code: create grant", "error", err)
		writeError(w, http.StatusInternalServerError, "server_error")
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"device_code":      dc.DeviceCode,
			"user_code":        dc.UserCode,
			"verification_uri": h.Cfg.Server.BaseURL + "/login/device",
			"expires_in":       dc.ExpiresIn,
			"interval":         dc.Interval,
		})
	}
}

// DeviceToken handles POST /api/auth/device/token.
func (h *Handler) DeviceToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, err := deviceParams(r)
	if err != nil || p.Get("grant_type") == "" || p.Get("device_code") == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if p.Get("grant_type") != deviceGrantType {
		writeError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	tok, err := h.Services.DeviceGrant.Poll(r.Context(), p.Get("device_code"))
	switch {
	case errors.Is(err, service.ErrAuthorizationPending), errors.Is(err, service.ErrSlowDown),
		errors.Is(err, service.ErrExpiredToken), errors.Is(err, service.ErrAccessDenied),
		errors.Is(err, service.ErrInvalidDeviceGrant):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		slog.Error("device token: poll", "error", err)
		writeError(w, http.StatusInternalServerError, "server_error")
	default:
		writeJSON(w, http.StatusOK, map[string]string{
			"access_token": tok.AccessToken,
			"token_type":   "bearer",
			"scope":        strings.Join(tok.Scopes, " "),
		})
	}
}
```

- [ ] **Step 5: Register the routes**

In `internal/router/router.go`, add constants beside the other limits and routes inside the `/api/auth` group:

```go
	deviceCodeLimit  = 20
	deviceCodeWindow = time.Hour
```

```go
		r.With(middleware.RateLimit(deviceCodeLimit, deviceCodeWindow)).Post("/device/code", h.DeviceCode)
		r.Post("/device/token", h.DeviceToken)
```

- [ ] **Step 6: Run the tests, then commit**

Run: `go test ./internal/router ./internal/middleware ./internal/handler -run 'DeviceLogin|CSRF' -v`
Expected: PASS.

```bash
git add internal/handler internal/router internal/middleware
git commit -m "feat(auth): device login code and token endpoints"
```

---

### Task 4: Docs and full verification

**Files:**
- Modify: `docs/api-reference.md`

- [ ] **Step 1: Document the endpoints**

In the `## Auth` area of `docs/api-reference.md`, add a `### Device login` subsection after the table:

````markdown
### Device login

`cz auth login` uses the device-code flow (RFC 8628). Neither endpoint needs a cookie, CSRF token or client secret, and every response carries `Cache-Control: no-store`. Both accept a URL-encoded form or JSON body, never the query string.

| Method | Path                     | Auth | Description                                                                                          |
| ------ | ------------------------ | ---- | ---------------------------------------------------------------------------------------------------- |
| POST   | `/api/auth/device/code`  | --   | Start a login: `scope` (space-separated, default `repo:write`) and `device_name`; 20 requests per hour per IP |
| POST   | `/api/auth/device/token` | --   | Poll: `grant_type=urn:ietf:params:oauth:grant-type:device_code` and `device_code`                     |

`/device/code` answers `{"device_code", "user_code", "verification_uri", "expires_in": 900, "interval": 5}`. `user_code` is `XXXX-XXXX`, and the user types it at `verification_uri` (`<host>/login/device`); no response or link carries it in a URL. An unknown scope or `repo:admin` is `400 invalid_scope`; a sixth unexpired grant from one IP is `429`.

`/device/token` answers `200 {"access_token", "token_type": "bearer", "scope"}` once the user has approved, exactly once. The token is a personal access token without an expiry, named `cz (<device_name>) · <date>` in Settings → Tokens. Everything else is `400 {"error": …}`:

| `error`                  | When                                                                                      |
| ------------------------ | ----------------------------------------------------------------------------------------- |
| `authorization_pending`  | The user hasn't decided yet; keep polling                                                  |
| `slow_down`              | Polled before `interval` elapsed; the interval grows by 5 s for this code                  |
| `expired_token`          | 900 s passed                                                                               |
| `access_denied`          | The user denied the login                                                                  |
| `invalid_grant`          | Unknown or already redeemed `device_code`                                                  |
| `unsupported_grant_type` | `grant_type` is anything but the device-code URN                                           |
| `invalid_request`        | `grant_type` or `device_code` missing, or an unreadable body                               |
| `server_error`           | Anything else; the cause is logged server-side                                             |
````

- [ ] **Step 2: Full verification**

Run: `gofmt -l internal` (expect no output), `make lint`, `go vet ./...`, `go test ./... -race` with `TEST_DATABASE_DSN` set.
Expected: all PASS. Then check the three 07a acceptance bullets by hand against the tests: lifecycle, `slow_down`, one-token concurrency, `czp_` token listed and revocable (the service test lists it; revoke is the existing `Delete`).

- [ ] **Step 3: Commit**

The 07a ticket file lives on `feat/cz-device-login` (the design PR), not on `main`. If that PR has merged by now, rebase this branch onto `origin/main`, then tick the criteria the tests cover and set `Status: done` in `.scratch/cz-cli/issues/07a-device-grant-api.md` in this same commit. If it hasn't, leave the ticket and tick it in a follow-up once it has.

```bash
git add docs/api-reference.md
git commit -m "docs(auth): document the device login endpoints"
```

---

## Self-Review

- **Spec coverage (07a):** migration/model/store/service (Tasks 1–2); both endpoints and error table (Task 3); CSRF exemption not touching `setup.go` (Task 3); limits — 20/h/IP via `RateLimit`, 5 live per IP in the store, interval in DB (Tasks 1, 3); lazy cleanup (Task 2 `Create`); lifecycle and each error response tested (Tasks 1–3); `api-reference.md` (Task 4). The design's "per-IP backstop on polling" is satisfied by the existing global anonymous `core` limiter (1000/h per IP), noted in Global Constraints; no extra middleware.
- **Placeholders:** none; the one "check before relying" step (`validateScopes`) names the exact behaviour required and the fallback.
- **Type consistency:** `Redeem`'s callback `func(ctx, *sql.Tx, *model.DeviceGrant) error` matches between store and service; `Create` signatures `(ctx, ip, scopes, deviceName)` match handler and tests; sentinel names match across service, handler and tests.
- **Test-only seam:** `SetClock` is exported for the external `service_test` package; the concurrent test depends on it so the second poll isn't `slow_down`.

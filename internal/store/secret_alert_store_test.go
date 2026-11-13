package store_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func secretFixture(t *testing.T) (*store.SecretAlertStore, *sql.DB, int64, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	repo := testutil.SeedRepo(t, db, owner, "owner"+suffix, suffix)
	return store.NewSecretAlertStore(db), db, owner, repo
}

func sighting(sha string, line int) model.SecretSighting {
	return model.SecretSighting{CommitSHA: sha, FilePath: "config.env", Line: line, Ref: "refs/heads/main"}
}

func TestSecretAlertStore_RecordDedupesOnHashAndAppendsLocations(t *testing.T) {
	s, _, _, repo := secretFixture(t)
	ctx := context.Background()

	a, created, err := s.Record(ctx, repo, "aws_access_key_id", "h1", "AKIA1234", model.SecretEventFound, sighting("c1", 3))
	if err != nil || !created || a.Number != 1 || a.Blocked {
		t.Fatalf("first Record = %+v, %v, %v; want new alert #1 that a ref reaches", a, created, err)
	}
	b, created, err := s.Record(ctx, repo, "aws_access_key_id", "h1", "AKIA1234", model.SecretEventFound, sighting("c2", 9))
	if err != nil || created || b.ID != a.ID {
		t.Fatalf("same hash = %+v, %v, %v; want the same alert, not new", b, created, err)
	}
	c, created, err := s.Record(ctx, repo, "github_token", "h2", "ghp_wxyz", model.SecretEventFound, sighting("c2", 4))
	if err != nil || !created || c.Number != 2 {
		t.Fatalf("other hash = %+v, %v, %v; want alert #2", c, created, err)
	}

	locs, _ := s.Locations(ctx, a.ID)
	events, _ := s.Events(ctx, a.ID)
	if len(locs) != 2 || len(events) != 2 {
		t.Errorf("alert #1 has %d locations and %d events, want 2 and 2", len(locs), len(events))
	}

	if _, _, err := s.Record(ctx, repo, "aws_access_key_id", "h1", "AKIA1234", model.SecretEventFound, sighting("c1", 3)); err != nil {
		t.Fatal(err)
	}
	if locs, _ := s.Locations(ctx, a.ID); len(locs) != 2 {
		t.Errorf("repeating a location left %d, want 2", len(locs))
	}
}

func TestSecretAlertStore_BlockedLocationIsPromotedWhenARefReachesIt(t *testing.T) {
	s, _, _, repo := secretFixture(t)
	ctx := context.Background()

	a, _, err := s.Record(ctx, repo, "github_token", "h", "ghp_wxyz", model.SecretEventBlocked, sighting("c1", 1))
	if err != nil || !a.Blocked {
		t.Fatalf("blocked Record = %+v, %v; want Blocked", a, err)
	}
	if n, _ := s.OpenCount(ctx, repo); n != 0 {
		t.Errorf("OpenCount = %d with only a blocked alert, want 0", n)
	}
	if err := s.MarkReached(ctx, repo, []string{"other", "c1"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, repo, a.Number)
	if got.Blocked {
		t.Error("alert still Blocked after MarkReached")
	}
	if n, _ := s.OpenCount(ctx, repo); n != 1 {
		t.Errorf("OpenCount = %d after MarkReached, want 1", n)
	}
}

func TestSecretAlertStore_MarkReachedStaysInItsRepository(t *testing.T) {
	s, db, owner, repo := secretFixture(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t) + "x"
	other := testutil.SeedRepo(t, db, owner, "o"+suffix, suffix)

	a, _, _ := s.Record(ctx, other, "github_token", "h", "ghp_wxyz", model.SecretEventBlocked, sighting("c1", 1))
	if err := s.MarkReached(ctx, repo, []string{"c1"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetByID(ctx, a.ID); !got.Blocked {
		t.Error("MarkReached on one repository reached into another")
	}
}

func TestSecretAlertStore_StateResolutionCheck(t *testing.T) {
	s, db, _, repo := secretFixture(t)
	ctx := context.Background()
	a, _, _ := s.Record(ctx, repo, "github_token", "h", "ghp_wxyz", model.SecretEventFound, sighting("c1", 1))

	for _, q := range []string{
		`UPDATE secret_scan_alerts SET state = 'closed' WHERE id = $1`,
		`UPDATE secret_scan_alerts SET resolution = 'revoked' WHERE id = $1`,
		`UPDATE secret_scan_alerts SET state = 'closed', resolution = 'wontfix' WHERE id = $1`,
	} {
		if _, err := db.ExecContext(ctx, q, a.ID); err == nil {
			t.Errorf("%s: accepted, want a CHECK violation", q)
		}
	}
}

func TestSecretAlertStore_CloseReopenEvents(t *testing.T) {
	s, _, owner, repo := secretFixture(t)
	ctx := context.Background()
	a, _, _ := s.Record(ctx, repo, "github_token", "h", "ghp_wxyz", model.SecretEventFound, sighting("c1", 1))

	if err := s.Close(ctx, a.ID, owner, model.SecretResolutionFalsePositive, model.SecretEventClosed, false); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetByID(ctx, a.ID)
	if got.State != "closed" || got.Resolution != "false_positive" || got.ResolvedBy == nil || *got.ResolvedBy != owner || got.ResolvedAt == nil {
		t.Errorf("closed alert = %+v", got)
	}
	if closed, _ := s.IsClosed(ctx, repo, "h"); !closed {
		t.Error("IsClosed = false for a closed alert")
	}
	if err := s.Close(ctx, a.ID, owner, "revoked", model.SecretEventClosed, false); !errors.Is(err, store.ErrSecretAlertState) {
		t.Errorf("closing twice: %v, want ErrSecretAlertState", err)
	}

	if err := s.Reopen(ctx, a.ID, owner); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetByID(ctx, a.ID)
	if got.State != "open" || got.Resolution != "" || got.ResolvedBy != nil || got.ResolvedAt != nil {
		t.Errorf("reopened alert = %+v, want resolution cleared", got)
	}
	if err := s.Reopen(ctx, a.ID, owner); !errors.Is(err, store.ErrSecretAlertState) {
		t.Errorf("reopening an open alert: %v, want ErrSecretAlertState", err)
	}
	if err := s.Reopen(ctx, -1, owner); !errors.Is(err, store.ErrSecretAlertNotFound) {
		t.Errorf("reopening a missing alert: %v, want ErrSecretAlertNotFound", err)
	}
}

func TestSecretAlertStore_CascadesAndNullsUsers(t *testing.T) {
	s, db, _, repo := secretFixture(t)
	ctx := context.Background()
	actor := testutil.SeedUser(t, db, testutil.UniqueSuffix(t)+"act")
	a, _, _ := s.Record(ctx, repo, "github_token", "h", "ghp_wxyz", model.SecretEventFound, sighting("c1", 1))
	if err := s.Close(ctx, a.ID, actor, model.SecretResolutionRevoked, model.SecretEventClosed, false); err != nil {
		t.Fatal(err)
	}

	testutil.Exec(t, db, `DELETE FROM users WHERE id = $1`, actor)
	got, err := s.GetByID(ctx, a.ID)
	if err != nil || got.ResolvedBy != nil {
		t.Fatalf("after deleting the resolver: %+v, %v; want resolved_by NULL", got, err)
	}
	events, _ := s.Events(ctx, a.ID)
	for _, e := range events {
		if e.ActorID != nil {
			t.Errorf("event %s kept actor %d after the user was deleted", e.Kind, *e.ActorID)
		}
	}

	testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repo)
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM secret_scan_alerts WHERE id = $1) + (SELECT COUNT(*) FROM secret_scan_locations WHERE alert_id = $1)
		 + (SELECT COUNT(*) FROM secret_scan_events WHERE alert_id = $1)`, a.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows left after deleting the repository (%v), want 0", n, err)
	}
}

func TestSecretAlertStore_ConcurrentRecordNumbersAndDedupes(t *testing.T) {
	s, _, _, repo := secretFixture(t)
	ctx := context.Background()

	const distinct, perHash = 8, 3
	var wg sync.WaitGroup
	var mu sync.Mutex
	createdBy := map[int]int{}
	for h := range distinct {
		for range perHash {
			wg.Add(1)
			go func() {
				defer wg.Done()
				hash := string(rune('a' + h))
				a, created, err := s.Record(ctx, repo, "github_token", hash, "ghp_wxyz", model.SecretEventFound, sighting("c"+hash, 1))
				if err != nil {
					t.Errorf("Record %s: %v", hash, err)
					return
				}
				if created {
					mu.Lock()
					createdBy[h]++
					mu.Unlock()
				}
				_ = a
			}()
		}
	}
	wg.Wait()

	for h := range distinct {
		if createdBy[h] != 1 {
			t.Errorf("hash %d created %d alerts, want 1", h, createdBy[h])
		}
	}
	alerts, total, err := s.List(ctx, repo, "", "", 1, 50)
	if err != nil || total != distinct {
		t.Fatalf("List = %d alerts (%v), want %d", total, err, distinct)
	}
	seen := map[int]bool{}
	for _, a := range alerts {
		seen[a.Number] = true
	}
	for n := 1; n <= distinct; n++ {
		if !seen[n] {
			t.Errorf("number %d missing from %v", n, seen)
		}
	}
}

func TestSecretAlertStore_ListFiltersAndPages(t *testing.T) {
	s, _, _, repo := secretFixture(t)
	ctx := context.Background()
	a, _, _ := s.Record(ctx, repo, "aws_access_key_id", "h1", "AKIA1234", model.SecretEventFound, sighting("c1", 1))
	_, _, _ = s.Record(ctx, repo, "github_token", "h2", "ghp_wxyz", model.SecretEventFound, sighting("c1", 2))
	_, _, _ = s.Record(ctx, repo, "github_token", "h3", "ghp_abcd", model.SecretEventFound, sighting("c1", 3))
	if err := s.Close(ctx, a.ID, 0, model.SecretResolutionRevoked, model.SecretEventAutoRevoked, true); err != nil {
		t.Fatal(err)
	}

	if _, total, _ := s.List(ctx, repo, "open", "", 1, 25); total != 2 {
		t.Errorf("open total = %d, want 2", total)
	}
	if got, total, _ := s.List(ctx, repo, "closed", "", 1, 25); total != 1 || !got[0].AutoRevoked || got[0].ResolvedBy != nil {
		t.Errorf("closed = %+v, want the one auto-revoked alert with no resolver", got)
	}
	if _, total, _ := s.List(ctx, repo, "", "github_token", 1, 25); total != 2 {
		t.Errorf("github_token total = %d, want 2", total)
	}
	page2, total, _ := s.List(ctx, repo, "", "", 2, 2)
	if total != 3 || len(page2) != 1 || page2[0].Number != 1 {
		t.Errorf("page 2 = %+v of %d, want alert #1 only", page2, total)
	}
}

func TestSecretAlertStore_SweepBlockedDeletesOnlyOldUnreachedOpenAlerts(t *testing.T) {
	s, db, _, repo := secretFixture(t)
	ctx := context.Background()
	old := time.Now().Add(-8 * 24 * time.Hour)

	mk := func(hash, kind string) *model.SecretScanAlert {
		a, _, err := s.Record(ctx, repo, "github_token", hash, "ghp_wxyz", kind, sighting("c"+hash, 1))
		if err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, db, `UPDATE secret_scan_alerts SET created_at = $2 WHERE id = $1`, a.ID, old)
		return a
	}
	staleBlocked := mk("a", model.SecretEventBlocked)
	reached := mk("b", model.SecretEventFound)
	closedBlocked := mk("c", model.SecretEventBlocked)
	if err := s.Close(ctx, closedBlocked.ID, 0, model.SecretResolutionFalsePositive, model.SecretEventAllowed, false); err != nil {
		t.Fatal(err)
	}
	fresh, _, _ := s.Record(ctx, repo, "github_token", "d", "ghp_wxyz", model.SecretEventBlocked, sighting("cd", 1))

	n, err := s.SweepBlocked(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("SweepBlocked = %d, %v; want 1", n, err)
	}
	for _, c := range []struct {
		a    *model.SecretScanAlert
		gone bool
	}{{staleBlocked, true}, {reached, false}, {closedBlocked, false}, {fresh, false}} {
		_, err := s.GetByID(ctx, c.a.ID)
		if gone := errors.Is(err, store.ErrSecretAlertNotFound); gone != c.gone {
			t.Errorf("alert #%d (hash %s): deleted = %v, want %v", c.a.Number, c.a.SecretHash, gone, c.gone)
		}
	}
}

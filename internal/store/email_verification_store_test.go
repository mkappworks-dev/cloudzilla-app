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

// seedVerifyUser seeds an unverified user and removes every token row tied to
// their address afterwards: deleting the user only nulls user_id.
func seedVerifyUser(t *testing.T, db *sql.DB) (id int64, email, suffix string) {
	t.Helper()
	suffix = testutil.UniqueSuffix(t)
	id = testutil.SeedUser(t, db, suffix)
	email = "testuser_" + suffix + "@test.invalid"
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM email_verification_tokens WHERE lower(email) = lower($1)`, email)
	})
	return id, email, suffix
}

func TestEmailVerificationStore_Issue(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewEmailVerificationStore(db)

	t.Run("returns the address and username and stores a pending link", func(t *testing.T) {
		id, email, suffix := seedVerifyUser(t, db)
		gotEmail, gotName, err := s.Issue(ctx, id, "h1_"+suffix, time.Hour, time.Minute)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if gotEmail != email || gotName != "testuser_"+suffix {
			t.Errorf("Issue = %q, %q; want %q, testuser_%s", gotEmail, gotName, email, suffix)
		}
		if pending, err := s.LinkPending(ctx, id); err != nil || !pending {
			t.Errorf("LinkPending = %v, %v; want true", pending, err)
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		if _, _, err := s.Issue(ctx, -1, "h_"+testutil.UniqueSuffix(t), time.Hour, time.Minute); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("Issue = %v; want sql.ErrNoRows", err)
		}
	})

	t.Run("verified user is refused", func(t *testing.T) {
		id, _, suffix := seedVerifyUser(t, db)
		testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, id)
		if _, _, err := s.Issue(ctx, id, "h_"+suffix, time.Hour, time.Minute); !errors.Is(err, store.ErrEmailAlreadyVerified) {
			t.Errorf("Issue = %v; want ErrEmailAlreadyVerified", err)
		}
	})

	t.Run("second issue inside the cooldown is refused and keeps the first link", func(t *testing.T) {
		id, _, suffix := seedVerifyUser(t, db)
		if _, _, err := s.Issue(ctx, id, "first_"+suffix, time.Hour, time.Hour); err != nil {
			t.Fatalf("first Issue: %v", err)
		}
		if _, _, err := s.Issue(ctx, id, "second_"+suffix, time.Hour, time.Hour); !errors.Is(err, store.ErrVerificationCooldown) {
			t.Fatalf("second Issue = %v; want ErrVerificationCooldown", err)
		}
		if link, _ := s.Lookup(ctx, "first_"+suffix); link.State != model.EmailVerificationPending {
			t.Errorf("first link state = %s; want pending", link.State)
		}
		if link, _ := s.Lookup(ctx, "second_"+suffix); link.State != model.EmailVerificationInvalid {
			t.Errorf("second link state = %s; want invalid", link.State)
		}
	})

	t.Run("cooldown survives the account being recreated with the same address", func(t *testing.T) {
		id, email, suffix := seedVerifyUser(t, db)
		if _, _, err := s.Issue(ctx, id, "first_"+suffix, time.Hour, time.Hour); err != nil {
			t.Fatalf("first Issue: %v", err)
		}
		testutil.DeleteUsers(t, db, id)
		otherSuffix := testutil.UniqueSuffix(t)
		otherID := testutil.SeedUser(t, db, otherSuffix)
		testutil.Exec(t, db, `UPDATE users SET email = upper($2) WHERE id = $1`, otherID, email)
		if _, _, err := s.Issue(ctx, otherID, "other_"+otherSuffix, time.Hour, time.Hour); !errors.Is(err, store.ErrVerificationCooldown) {
			t.Errorf("Issue for the same address in another case = %v; want ErrVerificationCooldown", err)
		}
	})

	t.Run("after the cooldown the old link is replaced", func(t *testing.T) {
		id, _, suffix := seedVerifyUser(t, db)
		if _, _, err := s.Issue(ctx, id, "old_"+suffix, time.Hour, time.Minute); err != nil {
			t.Fatalf("first Issue: %v", err)
		}
		testutil.Exec(t, db, `UPDATE email_verification_tokens SET created_at = NOW() - INTERVAL '2 minutes' WHERE token_hash = $1`, "old_"+suffix)
		if _, _, err := s.Issue(ctx, id, "new_"+suffix, time.Hour, time.Minute); err != nil {
			t.Fatalf("second Issue: %v", err)
		}
		if link, _ := s.Lookup(ctx, "old_"+suffix); link.State != model.EmailVerificationInvalid {
			t.Errorf("old link state = %s; want invalid", link.State)
		}
		if link, _ := s.Lookup(ctx, "new_"+suffix); link.State != model.EmailVerificationPending {
			t.Errorf("new link state = %s; want pending", link.State)
		}
	})

	t.Run("sweeps orphaned rows past the cooldown but keeps recent ones", func(t *testing.T) {
		id, _, suffix := seedVerifyUser(t, db)
		orphanEmail := "orphan_" + suffix + "@test.invalid"
		t.Cleanup(func() {
			testutil.Exec(t, db, `DELETE FROM email_verification_tokens WHERE email = $1`, orphanEmail)
		})
		testutil.Exec(t, db,
			`INSERT INTO email_verification_tokens (user_id, email, token_hash, expires_at, created_at)
			 VALUES (NULL, $1, $2, NOW() + INTERVAL '1 hour', NOW() - INTERVAL '2 minutes'),
			        (NULL, $1, $3, NOW() + INTERVAL '1 hour', NOW())`,
			orphanEmail, "stale_"+suffix, "fresh_"+suffix)
		if _, _, err := s.Issue(ctx, id, "mine_"+suffix, time.Hour, time.Minute); err != nil {
			t.Fatalf("Issue: %v", err)
		}
		var stale, fresh int
		if err := db.QueryRow(`SELECT COUNT(*) FILTER (WHERE token_hash = $1), COUNT(*) FILTER (WHERE token_hash = $2)
			FROM email_verification_tokens WHERE email = $3`, "stale_"+suffix, "fresh_"+suffix, orphanEmail).Scan(&stale, &fresh); err != nil {
			t.Fatal(err)
		}
		if stale != 0 || fresh != 1 {
			t.Errorf("stale=%d fresh=%d; want 0 and 1", stale, fresh)
		}
	})
}

func TestEmailVerificationStore_LookupAndLinkPending(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewEmailVerificationStore(db)

	tests := []struct {
		name        string
		mutate      string
		wantState   model.EmailVerificationState
		wantPending bool
	}{
		{"live", ``, model.EmailVerificationPending, true},
		{"expired", `UPDATE email_verification_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE token_hash = $1`, model.EmailVerificationExpired, false},
		{"used", `UPDATE email_verification_tokens SET used_at = NOW() WHERE token_hash = $1`, model.EmailVerificationInvalid, false},
		{"used and expired is invalid, not expired", `UPDATE email_verification_tokens SET used_at = NOW(), expires_at = NOW() - INTERVAL '1 second' WHERE token_hash = $1`, model.EmailVerificationInvalid, false},
		{"address changed", `UPDATE users SET email = 'changed_' || email WHERE id = (SELECT user_id FROM email_verification_tokens WHERE token_hash = $1)`, model.EmailVerificationInvalid, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, email, suffix := seedVerifyUser(t, db)
			hash := "h_" + suffix
			if _, _, err := s.Issue(ctx, id, hash, time.Hour, time.Minute); err != nil {
				t.Fatalf("Issue: %v", err)
			}
			if tt.mutate != "" {
				testutil.Exec(t, db, tt.mutate, hash)
			}
			link, err := s.Lookup(ctx, hash)
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if link.State != tt.wantState {
				t.Errorf("state = %s; want %s", link.State, tt.wantState)
			}
			if tt.wantState == model.EmailVerificationPending {
				if link.Email != email || link.Username != "testuser_"+suffix {
					t.Errorf("link = %+v; want pending for %q", link, email)
				}
			} else if link.Email != "" || link.Username != "" {
				t.Errorf("non-pending link leaks %+v", link)
			}
			pending, err := s.LinkPending(ctx, id)
			if err != nil || pending != tt.wantPending {
				t.Errorf("LinkPending = %v, %v; want %v", pending, err, tt.wantPending)
			}
		})
	}

	t.Run("unknown hash is invalid", func(t *testing.T) {
		link, err := s.Lookup(ctx, "nope_"+testutil.UniqueSuffix(t))
		if err != nil || link.State != model.EmailVerificationInvalid {
			t.Errorf("Lookup = %+v, %v; want invalid", link, err)
		}
	})

	t.Run("orphaned token is invalid", func(t *testing.T) {
		id, _, suffix := seedVerifyUser(t, db)
		if _, _, err := s.Issue(ctx, id, "h_"+suffix, time.Hour, time.Minute); err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, db, `UPDATE email_verification_tokens SET user_id = NULL WHERE token_hash = $1`, "h_"+suffix)
		link, err := s.Lookup(ctx, "h_"+suffix)
		if err != nil || link.State != model.EmailVerificationInvalid {
			t.Errorf("Lookup = %+v, %v; want invalid", link, err)
		}
	})
}

func TestEmailVerificationStore_Consume(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewEmailVerificationStore(db)

	t.Run("verifies the user and spends the link", func(t *testing.T) {
		id, _, suffix := seedVerifyUser(t, db)
		hash := "h_" + suffix
		if _, _, err := s.Issue(ctx, id, hash, time.Hour, time.Minute); err != nil {
			t.Fatal(err)
		}
		state, u, err := s.Consume(ctx, hash)
		if err != nil {
			t.Fatalf("Consume: %v", err)
		}
		if state != model.EmailVerificationVerified || u == nil || u.ID != id || u.EmailVerifiedAt == nil {
			t.Fatalf("Consume = %s, %+v; want verified user %d", state, u, id)
		}
		if pending, _ := s.LinkPending(ctx, id); pending {
			t.Error("link is still pending after Consume")
		}
		state, u, err = s.Consume(ctx, hash)
		if err != nil || state != model.EmailVerificationInvalid || u != nil {
			t.Errorf("second Consume = %s, %v, %v; want invalid and no user", state, u, err)
		}
	})

	t.Run("spends every live token of the user", func(t *testing.T) {
		id, email, suffix := seedVerifyUser(t, db)
		if _, _, err := s.Issue(ctx, id, "a_"+suffix, time.Hour, time.Minute); err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, db,
			`INSERT INTO email_verification_tokens (user_id, email, token_hash, expires_at) VALUES ($1, $2, $3, NOW() + INTERVAL '1 hour')`,
			id, email, "b_"+suffix)
		if state, _, err := s.Consume(ctx, "a_"+suffix); err != nil || state != model.EmailVerificationVerified {
			t.Fatalf("Consume = %s, %v", state, err)
		}
		if link, _ := s.Lookup(ctx, "b_"+suffix); link.State != model.EmailVerificationInvalid {
			t.Errorf("sibling link state = %s; want invalid", link.State)
		}
	})

	t.Run("keeps the original verification time", func(t *testing.T) {
		id, _, suffix := seedVerifyUser(t, db)
		if _, _, err := s.Issue(ctx, id, "h_"+suffix, time.Hour, time.Minute); err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() - INTERVAL '1 day' WHERE id = $1`, id)
		var before time.Time
		if err := db.QueryRow(`SELECT email_verified_at FROM users WHERE id = $1`, id).Scan(&before); err != nil {
			t.Fatal(err)
		}
		_, u, err := s.Consume(ctx, "h_"+suffix)
		if err != nil || u == nil {
			t.Fatalf("Consume = %v, %v", u, err)
		}
		if !u.EmailVerifiedAt.Equal(before) {
			t.Errorf("verified_at moved from %v to %v", before, u.EmailVerifiedAt)
		}
	})

	refused := []struct {
		name      string
		mutate    string
		wantState model.EmailVerificationState
	}{
		{"expired", `UPDATE email_verification_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE token_hash = $1`, model.EmailVerificationExpired},
		{"used", `UPDATE email_verification_tokens SET used_at = NOW() WHERE token_hash = $1`, model.EmailVerificationInvalid},
		{"address changed", `UPDATE users SET email = 'changed_' || email WHERE id = (SELECT user_id FROM email_verification_tokens WHERE token_hash = $1)`, model.EmailVerificationInvalid},
		{"orphaned", `UPDATE email_verification_tokens SET user_id = NULL WHERE token_hash = $1`, model.EmailVerificationInvalid},
	}
	for _, tt := range refused {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			id, _, suffix := seedVerifyUser(t, db)
			hash := "h_" + suffix
			if _, _, err := s.Issue(ctx, id, hash, time.Hour, time.Minute); err != nil {
				t.Fatal(err)
			}
			testutil.Exec(t, db, tt.mutate, hash)
			state, u, err := s.Consume(ctx, hash)
			if err != nil || state != tt.wantState || u != nil {
				t.Errorf("Consume = %s, %v, %v; want %s and no user", state, u, err, tt.wantState)
			}
			var verified bool
			if err := db.QueryRow(`SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&verified); err != nil {
				t.Fatal(err)
			}
			if verified {
				t.Error("a refused Consume verified the user")
			}
		})
	}

	t.Run("unknown hash is invalid", func(t *testing.T) {
		state, u, err := s.Consume(ctx, "nope_"+testutil.UniqueSuffix(t))
		if err != nil || state != model.EmailVerificationInvalid || u != nil {
			t.Errorf("Consume = %s, %v, %v", state, u, err)
		}
	})
}

func TestEmailVerificationStore_ClosedDBErrors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := store.NewEmailVerificationStore(db)

	if _, _, err := s.Issue(ctx, 1, "h", time.Hour, time.Minute); err == nil {
		t.Error("Issue on a closed db must fail")
	}
	if _, err := s.Lookup(ctx, "h"); err == nil {
		t.Error("Lookup on a closed db must fail")
	}
	if _, err := s.LinkPending(ctx, 1); err == nil {
		t.Error("LinkPending on a closed db must fail")
	}
	if _, _, err := s.Consume(ctx, "h"); err == nil {
		t.Error("Consume on a closed db must fail")
	}
}

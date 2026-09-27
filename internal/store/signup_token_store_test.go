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

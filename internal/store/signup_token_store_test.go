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

func TestSignupTokenStore_Issue_PrunesLinksExpiredOverADayAgo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	stale := "stale_" + suffix + "@test.invalid"
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignupTokens(t, db, stale)
	cleanupSignupTokens(t, db, email)
	testutil.Exec(t, db, `INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ($1, $2, NOW() - INTERVAL '2 days')`, "stale_"+suffix, stale)

	issueSignupToken(t, store.NewSignupTokenStore(db), email, "fresh_"+suffix)

	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM signup_tokens WHERE email = $1`, stale).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Error("a link expired over a day ago must be pruned")
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

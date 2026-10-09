package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// createUser inserts a user through UserStore.Create and deletes it when the test ends.
func createUser(t *testing.T, db *sql.DB, username, email string) *model.User {
	t.Helper()
	u := &model.User{Username: username, Email: email, PasswordHash: "hash"}
	if err := store.NewUserStore(db).Create(context.Background(), u); err != nil {
		t.Fatalf("Create %s: %v", username, err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	return u
}

func TestUserStore_Create_Conflicts(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	base := createUser(t, db, "Conflict_"+suffix, "Conflict_"+suffix+"@test.invalid")

	orgName := "conflictorg_" + suffix
	org := &model.Organization{Name: orgName}
	if err := store.NewOrgStore(db).Create(ctx, org); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)

	tests := []struct {
		name     string
		username string
		email    string
		want     error
	}{
		{"username in another case", "CONFLICT_" + suffix, "other1_" + suffix + "@test.invalid", store.ErrUsernameTaken},
		{"username of an org", "CONFLICTORG_" + suffix, "other2_" + suffix + "@test.invalid", store.ErrUsernameTaken},
		{"same email", "other3_" + suffix, base.Email, store.ErrEmailTaken},
		{"email in another case", "other4_" + suffix, "CONFLICT_" + suffix + "@TEST.invalid", store.ErrEmailTaken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &model.User{Username: tt.username, Email: tt.email, PasswordHash: "x"}
			if err := s.Create(ctx, u); !errors.Is(err, tt.want) {
				testutil.DeleteUsers(t, db, u.ID)
				t.Errorf("Create = %v; want %v", err, tt.want)
			}
		})
	}
}

func TestUserStore_OwnerNameTaken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	u := createUser(t, db, "ownername_"+suffix, "ownername_"+suffix+"@test.invalid")
	org := &model.Organization{Name: "ownerorg_" + suffix}
	if err := store.NewOrgStore(db).Create(ctx, org); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)

	for name, tt := range map[string]struct {
		in   string
		want bool
	}{
		"user":          {u.Username, true},
		"user any case": {"OWNERNAME_" + suffix, true},
		"org":           {org.Name, true},
		"org any case":  {"OWNERORG_" + suffix, true},
		"free":          {"free_" + suffix, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := s.OwnerNameTaken(ctx, tt.in)
			if err != nil || got != tt.want {
				t.Errorf("OwnerNameTaken(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestUserStore_CreateFromInvitation(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)

	t.Run("claims the invitation and creates the user", func(t *testing.T) {
		suffix := testutil.UniqueSuffix(t)
		email := "invitee_" + suffix + "@test.invalid"
		invID, _ := testutil.SeedInvitation(t, db, email, time.Now().Add(time.Hour))
		u := &model.User{Username: "invitee_" + suffix, Email: email, PasswordHash: "x", IsInvited: true}
		if err := s.CreateFromInvitation(ctx, u, invID); err != nil {
			t.Fatalf("CreateFromInvitation: %v", err)
		}
		t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
		if u.ID == 0 || !u.IsInvited {
			t.Errorf("user = %+v", u)
		}
		var accepted bool
		if err := db.QueryRow(`SELECT accepted_at IS NOT NULL FROM invitations WHERE id = $1`, invID).Scan(&accepted); err != nil || !accepted {
			t.Errorf("invitation accepted = %v, %v", accepted, err)
		}
		again := &model.User{Username: "again_" + suffix, Email: "again_" + suffix + "@test.invalid", PasswordHash: "x"}
		if err := s.CreateFromInvitation(ctx, again, invID); !errors.Is(err, store.ErrInvitationUnusable) {
			t.Errorf("second claim = %v; want ErrInvitationUnusable", err)
		}
	})

	t.Run("expired invitation creates nothing", func(t *testing.T) {
		suffix := testutil.UniqueSuffix(t)
		invID, _ := testutil.SeedInvitation(t, db, "expired_"+suffix+"@test.invalid", time.Now().Add(-time.Hour))
		u := &model.User{Username: "expired_" + suffix, Email: "expired_" + suffix + "@test.invalid", PasswordHash: "x"}
		if err := s.CreateFromInvitation(ctx, u, invID); !errors.Is(err, store.ErrInvitationUnusable) {
			t.Fatalf("CreateFromInvitation = %v; want ErrInvitationUnusable", err)
		}
		if taken, _ := s.OwnerNameTaken(ctx, u.Username); taken {
			t.Error("the user was inserted despite the failed claim")
		}
	})

	t.Run("a taken username releases the claim", func(t *testing.T) {
		suffix := testutil.UniqueSuffix(t)
		email := "release_" + suffix + "@test.invalid"
		invID, _ := testutil.SeedInvitation(t, db, email, time.Now().Add(time.Hour))
		existing := createUser(t, db, "release_"+suffix, "release_other_"+suffix+"@test.invalid")
		u := &model.User{Username: existing.Username, Email: email, PasswordHash: "x"}
		if err := s.CreateFromInvitation(ctx, u, invID); !errors.Is(err, store.ErrUsernameTaken) {
			t.Fatalf("CreateFromInvitation = %v; want ErrUsernameTaken", err)
		}
		var accepted bool
		if err := db.QueryRow(`SELECT accepted_at IS NOT NULL FROM invitations WHERE id = $1`, invID).Scan(&accepted); err != nil || accepted {
			t.Errorf("failed signup spent the invitation: accepted=%v err=%v", accepted, err)
		}
	})

	t.Run("a registered email reads as unusable", func(t *testing.T) {
		suffix := testutil.UniqueSuffix(t)
		email := "taken_" + suffix + "@test.invalid"
		invID, _ := testutil.SeedInvitation(t, db, email, time.Now().Add(time.Hour))
		createUser(t, db, "taken_"+suffix, email)
		u := &model.User{Username: "taken2_" + suffix, Email: email, PasswordHash: "x"}
		if err := s.CreateFromInvitation(ctx, u, invID); !errors.Is(err, store.ErrInvitationUnusable) {
			t.Errorf("CreateFromInvitation = %v; want ErrInvitationUnusable", err)
		}
	})
}

func TestUserStore_CreateFromSignupToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	tokens := store.NewSignupTokenStore(db)

	t.Run("creates a verified user with the link's email", func(t *testing.T) {
		suffix := testutil.UniqueSuffix(t)
		email := "signup_" + suffix + "@test.invalid"
		cleanupSignupTokens(t, db, email)
		if ok, err := tokens.Issue(ctx, email, "hash_"+suffix, time.Now().Add(time.Hour)); err != nil || !ok {
			t.Fatalf("Issue = %v, %v", ok, err)
		}
		u := &model.User{Username: "signup_" + suffix, Email: "ignored@test.invalid", PasswordHash: "x"}
		if err := s.CreateFromSignupToken(ctx, u, "hash_"+suffix); err != nil {
			t.Fatalf("CreateFromSignupToken: %v", err)
		}
		t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
		if u.Email != email || u.EmailVerifiedAt == nil {
			t.Errorf("user email=%q verified=%v; want %q verified", u.Email, u.EmailVerifiedAt, email)
		}
		again := &model.User{Username: "signup2_" + suffix, PasswordHash: "x"}
		if err := s.CreateFromSignupToken(ctx, again, "hash_"+suffix); !errors.Is(err, store.ErrSignupTokenUnusable) {
			t.Errorf("second claim = %v; want ErrSignupTokenUnusable", err)
		}
	})

	t.Run("unknown token", func(t *testing.T) {
		u := &model.User{Username: "nobody_" + testutil.UniqueSuffix(t), PasswordHash: "x"}
		if err := s.CreateFromSignupToken(ctx, u, "nope_"+testutil.UniqueSuffix(t)); !errors.Is(err, store.ErrSignupTokenUnusable) {
			t.Errorf("CreateFromSignupToken = %v; want ErrSignupTokenUnusable", err)
		}
	})

	t.Run("a taken username releases the claim", func(t *testing.T) {
		suffix := testutil.UniqueSuffix(t)
		email := "signuprel_" + suffix + "@test.invalid"
		cleanupSignupTokens(t, db, email)
		if ok, err := tokens.Issue(ctx, email, "hash_"+suffix, time.Now().Add(time.Hour)); err != nil || !ok {
			t.Fatalf("Issue = %v, %v", ok, err)
		}
		existing := createUser(t, db, "signuprel_"+suffix, "signuprel_other_"+suffix+"@test.invalid")
		u := &model.User{Username: existing.Username, PasswordHash: "x"}
		if err := s.CreateFromSignupToken(ctx, u, "hash_"+suffix); !errors.Is(err, store.ErrUsernameTaken) {
			t.Fatalf("CreateFromSignupToken = %v; want ErrUsernameTaken", err)
		}
		if _, err := tokens.GetUsableByHash(ctx, "hash_"+suffix); err != nil {
			t.Errorf("failed signup spent the link: %v", err)
		}
	})
}

func TestUserStore_Lookups(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	u := createUser(t, db, "lookup_"+suffix, "Lookup_"+suffix+"@test.invalid")

	for name, get := range map[string]func(string) (*model.User, error){
		"GetByEmail":         func(e string) (*model.User, error) { return s.GetByEmail(ctx, e) },
		"GetByEmailWithRole": func(e string) (*model.User, error) { return s.GetByEmailWithRole(ctx, e) },
		"GetByEmailWithTOTP": func(e string) (*model.User, error) { return s.GetByEmailWithTOTP(ctx, e) },
	} {
		t.Run(name+" ignores case", func(t *testing.T) {
			got, err := get("LOOKUP_" + suffix + "@test.invalid")
			if err != nil || got.ID != u.ID {
				t.Errorf("got %v, %v; want user %d", got, err, u.ID)
			}
		})
		t.Run(name+" unknown", func(t *testing.T) {
			if _, err := get("nobody_" + suffix + "@test.invalid"); !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("err = %v; want sql.ErrNoRows", err)
			}
		})
	}

	t.Run("GetByUsernameWithTOTP", func(t *testing.T) {
		got, err := s.GetByUsernameWithTOTP(ctx, u.Username)
		if err != nil || got.ID != u.ID || got.TOTPEnabled {
			t.Errorf("got %+v, %v", got, err)
		}
		if _, err := s.GetByUsernameWithTOTP(ctx, "nobody_"+suffix); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("unknown: %v; want sql.ErrNoRows", err)
		}
	})

	t.Run("the ghost is hidden from typed lookups but loads by id", func(t *testing.T) {
		var ghostID int64
		var ghostName, ghostEmail string
		if err := db.QueryRow(`SELECT id, username, email FROM users WHERE id = ghost_user_id()`).Scan(&ghostID, &ghostName, &ghostEmail); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetByUsername(ctx, ghostName); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("GetByUsername(ghost) = %v; want sql.ErrNoRows", err)
		}
		if _, err := s.GetByEmail(ctx, ghostEmail); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("GetByEmail(ghost) = %v; want sql.ErrNoRows", err)
		}
		if got, err := s.GetByID(ctx, ghostID); err != nil || got.Username != ghostName {
			t.Errorf("GetByID(ghost) = %v, %v", got, err)
		}
		if m, err := s.GetManyByUsernames(ctx, []string{ghostName}); err != nil || len(m) != 0 {
			t.Errorf("GetManyByUsernames(ghost) = %v, %v", m, err)
		}
	})
}

func TestUserStore_CountAccounts_ExcludesGhost(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)

	if n, err := s.CountAccounts(ctx); err != nil || n != 0 {
		t.Fatalf("CountAccounts on an empty instance = %d, %v; want 0", n, err)
	}
	if _, err := s.CreateSuperadmin(ctx, "root", "root@test.invalid", "hash"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountAccounts(ctx); n != 1 {
		t.Errorf("CountAccounts = %d; want 1", n)
	}
}

func TestUserStore_CreateSuperadmin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)

	u, err := s.CreateSuperadmin(ctx, "super_"+suffix, "super_"+suffix+"@test.invalid", "hash")
	if err != nil {
		t.Fatalf("CreateSuperadmin: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	if !u.IsSuperadmin || u.PasswordHash != "hash" || u.Username != "super_"+suffix {
		t.Errorf("user = %+v", u)
	}
	if _, err := s.CreateSuperadmin(ctx, "SUPER_"+suffix, "other_"+suffix+"@test.invalid", "hash"); !errors.Is(err, store.ErrUsernameTaken) {
		t.Errorf("duplicate username = %v; want ErrUsernameTaken", err)
	}
	if _, err := s.CreateSuperadmin(ctx, "other_"+suffix, u.Email, "hash"); err == nil {
		t.Error("duplicate email must fail")
	}
}

func TestUserStore_OAuth(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)

	t.Run("CreateOAuthUser and GetByOAuthID", func(t *testing.T) {
		u, err := s.CreateOAuthUser(ctx, "oauth_"+suffix, "oauth_"+suffix+"@test.invalid", "github", "gh_"+suffix, "http://a/b.png", true)
		if err != nil {
			t.Fatalf("CreateOAuthUser: %v", err)
		}
		t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
		if u.PasswordHash != "" || u.OAuthProvider != "github" || u.OAuthID != "gh_"+suffix || u.AvatarURL != "http://a/b.png" || u.EmailVerifiedAt == nil {
			t.Errorf("user = %+v", u)
		}
		got, err := s.GetByOAuthID(ctx, "github", "gh_"+suffix)
		if err != nil || got.ID != u.ID {
			t.Errorf("GetByOAuthID = %v, %v", got, err)
		}
		if _, err := s.GetByOAuthID(ctx, "google", "gh_"+suffix); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("other provider: %v; want sql.ErrNoRows", err)
		}
		if _, err := s.CreateOAuthUser(ctx, "OAUTH_"+suffix, "x_"+suffix+"@test.invalid", "github", "gh2_"+suffix, "", false); !errors.Is(err, store.ErrUsernameTaken) {
			t.Errorf("duplicate username = %v; want ErrUsernameTaken", err)
		}
	})

	t.Run("CreateOAuthUser unverified email", func(t *testing.T) {
		u, err := s.CreateOAuthUser(ctx, "oauthu_"+suffix, "oauthu_"+suffix+"@test.invalid", "github", "ghu_"+suffix, "", false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
		if u.EmailVerifiedAt != nil {
			t.Errorf("verified_at = %v; want nil", u.EmailVerifiedAt)
		}
	})

	t.Run("LinkOAuth", func(t *testing.T) {
		u := createUser(t, db, "link_"+suffix, "link_"+suffix+"@test.invalid")
		other := createUser(t, db, "link2_"+suffix, "link2_"+suffix+"@test.invalid")
		if ok, err := s.LinkOAuth(ctx, u.ID, "github", "id_"+suffix); err != nil || !ok {
			t.Fatalf("LinkOAuth = %v, %v", ok, err)
		}
		if ok, err := s.LinkOAuth(ctx, u.ID, "google", "id2_"+suffix); err != nil || ok {
			t.Errorf("second LinkOAuth = %v, %v; want false, nil", ok, err)
		}
		if _, err := s.LinkOAuth(ctx, other.ID, "github", "id_"+suffix); !errors.Is(err, store.ErrOAuthIdentityTaken) {
			t.Errorf("identity linked elsewhere = %v; want ErrOAuthIdentityTaken", err)
		}
		if ok, err := s.LinkOAuth(ctx, -1, "github", "id3_"+suffix); err != nil || ok {
			t.Errorf("unknown user = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("UnlinkOAuth", func(t *testing.T) {
		withPassword := createUser(t, db, "unl_"+suffix, "unl_"+suffix+"@test.invalid")
		if _, err := s.LinkOAuth(ctx, withPassword.ID, "github", "unl_"+suffix); err != nil {
			t.Fatal(err)
		}
		if ok, err := s.UnlinkOAuth(ctx, withPassword.ID, "google"); err != nil || ok {
			t.Errorf("wrong provider = %v, %v; want false, nil", ok, err)
		}
		if ok, err := s.UnlinkOAuth(ctx, withPassword.ID, "github"); err != nil || !ok {
			t.Errorf("UnlinkOAuth = %v, %v; want true", ok, err)
		}
		if ok, err := s.UnlinkOAuth(ctx, withPassword.ID, "github"); err != nil || ok {
			t.Errorf("already unlinked = %v, %v; want false", ok, err)
		}

		oauthOnly, err := s.CreateOAuthUser(ctx, "unlonly_"+suffix, "unlonly_"+suffix+"@test.invalid", "github", "unlonly_"+suffix, "", true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testutil.DeleteUsers(t, db, oauthOnly.ID) })
		if ok, err := s.UnlinkOAuth(ctx, oauthOnly.ID, "github"); err != nil || ok {
			t.Errorf("passwordless account unlinked = %v, %v; want false (its only sign-in)", ok, err)
		}
	})

	t.Run("LinkOAuthByVerifiedEmail", func(t *testing.T) {
		verified := createUser(t, db, "lv_"+suffix, "lv_"+suffix+"@test.invalid")
		if ok, err := s.MarkEmailVerified(ctx, verified.ID, verified.Email); err != nil || !ok {
			t.Fatalf("MarkEmailVerified = %v, %v", ok, err)
		}
		got, err := s.LinkOAuthByVerifiedEmail(ctx, verified.ID, verified.Email, "github", "lv_"+suffix)
		if err != nil || got.OAuthProvider != "github" || got.OAuthID != "lv_"+suffix {
			t.Fatalf("LinkOAuthByVerifiedEmail = %+v, %v", got, err)
		}
		if _, err := s.LinkOAuthByVerifiedEmail(ctx, verified.ID, verified.Email, "google", "lv2_"+suffix); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("already linked = %v; want sql.ErrNoRows", err)
		}

		unverified := createUser(t, db, "lu_"+suffix, "lu_"+suffix+"@test.invalid")
		if _, err := s.LinkOAuthByVerifiedEmail(ctx, unverified.ID, unverified.Email, "github", "lu_"+suffix); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("unverified email = %v; want sql.ErrNoRows", err)
		}

		changed := createUser(t, db, "lc_"+suffix, "lc_"+suffix+"@test.invalid")
		if _, err := s.MarkEmailVerified(ctx, changed.ID, changed.Email); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LinkOAuthByVerifiedEmail(ctx, changed.ID, "stale_"+suffix+"@test.invalid", "github", "lc_"+suffix); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("email no longer current = %v; want sql.ErrNoRows", err)
		}

		noPassword, err := s.CreateOAuthUser(ctx, "lnp_"+suffix, "lnp_"+suffix+"@test.invalid", "", "", "", true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testutil.DeleteUsers(t, db, noPassword.ID) })
		if _, err := s.LinkOAuthByVerifiedEmail(ctx, noPassword.ID, noPassword.Email, "github", "lnp_"+suffix); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("passwordless account = %v; want sql.ErrNoRows", err)
		}

		taken := createUser(t, db, "lt_"+suffix, "lt_"+suffix+"@test.invalid")
		if _, err := s.MarkEmailVerified(ctx, taken.ID, taken.Email); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LinkOAuthByVerifiedEmail(ctx, taken.ID, taken.Email, "github", "lv_"+suffix); !errors.Is(err, store.ErrOAuthIdentityTaken) {
			t.Errorf("identity linked elsewhere = %v; want ErrOAuthIdentityTaken", err)
		}
	})
}

func TestUserStore_MarkEmailVerified(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	u := createUser(t, db, "mev_"+suffix, "mev_"+suffix+"@test.invalid")

	if ok, err := s.MarkEmailVerified(ctx, u.ID, "other_"+suffix+"@test.invalid"); err != nil || ok {
		t.Errorf("not the user's address = %v, %v; want false", ok, err)
	}
	if ok, err := s.MarkEmailVerified(ctx, u.ID, "MEV_"+suffix+"@test.invalid"); err != nil || !ok {
		t.Fatalf("MarkEmailVerified (any case) = %v, %v; want true", ok, err)
	}
	var first time.Time
	if err := db.QueryRow(`SELECT email_verified_at FROM users WHERE id = $1`, u.ID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = $2 WHERE id = $1`, u.ID, first.Add(-time.Hour))
	if ok, err := s.MarkEmailVerified(ctx, u.ID, u.Email); err != nil || !ok {
		t.Fatalf("repeat = %v, %v", ok, err)
	}
	var after time.Time
	if err := db.QueryRow(`SELECT email_verified_at FROM users WHERE id = $1`, u.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(first.Add(-time.Hour)) {
		t.Errorf("verified_at moved to %v; want it kept", after)
	}
}

func TestUserStore_ClaimReauthAttempt(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	claim := func() bool {
		t.Helper()
		ok, err := s.ClaimReauthAttempt(ctx, userID, 3, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	for i := 1; i <= 3; i++ {
		if !claim() {
			t.Fatalf("attempt %d refused; limit is 3", i)
		}
	}
	if claim() {
		t.Error("attempt 4 allowed; want refused once the limit is spent")
	}
	if err := s.ReleaseReauthAttempt(ctx, userID); err != nil {
		t.Fatal(err)
	}
	if !claim() {
		t.Error("a released attempt must free a slot")
	}

	testutil.Exec(t, db, `UPDATE users SET reauth_window_start = NOW() - INTERVAL '2 hours' WHERE id = $1`, userID)
	if !claim() {
		t.Fatal("a claim after the window must be allowed")
	}
	var failures int
	if err := db.QueryRow(`SELECT reauth_failures FROM users WHERE id = $1`, userID).Scan(&failures); err != nil || failures != 1 {
		t.Errorf("failures = %d, %v; want the window reset to 1", failures, err)
	}

	for i := 0; i < 5; i++ {
		if err := s.ReleaseReauthAttempt(ctx, userID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(`SELECT reauth_failures FROM users WHERE id = $1`, userID).Scan(&failures); err != nil || failures != 0 {
		t.Errorf("failures = %d, %v; want release floored at 0", failures, err)
	}

	if ok, err := s.ClaimReauthAttempt(ctx, -1, 3, time.Hour); err != nil || ok {
		t.Errorf("unknown user = %v, %v; want false, nil", ok, err)
	}
}

func TestUserStore_IssueReauthCode(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	issue := func(hash string, gap time.Duration, perWindow int) bool {
		t.Helper()
		ok, err := s.IssueReauthCode(ctx, userID, hash, time.Hour, gap, perWindow, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	ageLastSend := func() {
		testutil.Exec(t, db, `UPDATE users SET reauth_code_sent_at = NOW() - INTERVAL '10 minutes' WHERE id = $1`, userID)
	}

	if !issue("h1", time.Minute, 2) {
		t.Fatal("first code refused")
	}
	if got, err := s.LiveReauthCode(ctx, userID); err != nil || got != "h1" {
		t.Errorf("LiveReauthCode = %q, %v; want h1", got, err)
	}
	if issue("h2", time.Minute, 2) {
		t.Error("a second code inside the gap was issued")
	}
	if got, _ := s.LiveReauthCode(ctx, userID); got != "h1" {
		t.Errorf("a refused issue replaced the code: %q", got)
	}
	ageLastSend()
	if !issue("h2", time.Minute, 2) {
		t.Fatal("second code after the gap refused")
	}
	if got, _ := s.LiveReauthCode(ctx, userID); got != "h2" {
		t.Errorf("code = %q; want h2 (replaces h1)", got)
	}
	ageLastSend()
	if issue("h3", time.Minute, 2) {
		t.Error("third code in the window was issued; cap is 2")
	}
	testutil.Exec(t, db, `UPDATE users SET reauth_codes_window_start = NOW() - INTERVAL '2 hours' WHERE id = $1`, userID)
	if !issue("h3", time.Minute, 2) {
		t.Error("a code after the window must be allowed")
	}
	var sent int
	if err := db.QueryRow(`SELECT reauth_codes_sent FROM users WHERE id = $1`, userID).Scan(&sent); err != nil || sent != 1 {
		t.Errorf("codes sent = %d, %v; want the window reset to 1", sent, err)
	}

	if ok, err := s.IssueReauthCode(ctx, -1, "h", time.Hour, time.Minute, 2, time.Hour); err != nil || ok {
		t.Errorf("unknown user = %v, %v; want false, nil", ok, err)
	}
}

func TestUserStore_ReauthCodeLifecycle(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	if got, err := s.LiveReauthCode(ctx, userID); err != nil || got != "" {
		t.Errorf("no code: LiveReauthCode = %q, %v; want empty", got, err)
	}
	if _, err := s.LiveReauthCode(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user = %v; want sql.ErrNoRows", err)
	}

	if err := s.StoreReauthCode(ctx, userID, "code", time.Hour); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SpendReauthCode(ctx, userID, "wrong"); err != nil || ok {
		t.Errorf("wrong hash = %v, %v; want false", ok, err)
	}
	if ok, err := s.SpendReauthCode(ctx, userID, "code"); err != nil || !ok {
		t.Fatalf("SpendReauthCode = %v, %v; want true", ok, err)
	}
	if ok, _ := s.SpendReauthCode(ctx, userID, "code"); ok {
		t.Error("a spent code was spent twice")
	}
	if got, _ := s.LiveReauthCode(ctx, userID); got != "" {
		t.Errorf("spent code still live: %q", got)
	}

	if err := s.StoreReauthCode(ctx, userID, "old", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.StoreReauthCode(ctx, userID, "new", time.Hour); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SpendReauthCode(ctx, userID, "old"); ok {
		t.Error("a replaced code was accepted")
	}

	testutil.Exec(t, db, `UPDATE users SET reauth_code_expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, userID)
	if got, _ := s.LiveReauthCode(ctx, userID); got != "" {
		t.Errorf("expired code reported live: %q", got)
	}
	if ok, _ := s.SpendReauthCode(ctx, userID, "new"); ok {
		t.Error("an expired code was accepted")
	}

	if err := s.StoreReauthCode(ctx, userID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SpendReauthCode(ctx, userID, ""); ok {
		t.Error("the empty hash must never match")
	}
}

func TestUserStore_SessionState(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))

	v0, err := s.SessionVersion(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.BumpSessionVersion(ctx, userID)
	if err != nil || v1 != v0+1 {
		t.Fatalf("BumpSessionVersion = %d, %v; want %d", v1, err, v0+1)
	}
	st, err := s.SessionState(ctx, userID)
	if err != nil || st.Version != v1 || !st.IsSuperadmin {
		t.Errorf("SessionState = %+v, %v", st, err)
	}

	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)
	if _, err := s.SessionState(ctx, userID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("suspended SessionState = %v; want sql.ErrNoRows", err)
	}
	if v, err := s.SessionVersion(ctx, userID); err != nil || v != v1 {
		t.Errorf("SessionVersion of a suspended user = %d, %v; want %d", v, err, v1)
	}

	if _, err := s.BumpSessionVersion(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Bump unknown = %v; want sql.ErrNoRows", err)
	}
	if _, err := s.SessionVersion(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SessionVersion unknown = %v; want sql.ErrNoRows", err)
	}
}

func TestUserStore_ChangePassword(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	before, _ := s.SessionVersion(ctx, userID)

	if ok, err := s.ChangePassword(ctx, userID, "not-x", "new"); err != nil || ok {
		t.Errorf("stale old hash = %v, %v; want false", ok, err)
	}
	if v, _ := s.SessionVersion(ctx, userID); v != before {
		t.Error("a refused change bumped the session version")
	}
	if ok, err := s.ChangePassword(ctx, userID, "x", "new"); err != nil || !ok {
		t.Fatalf("ChangePassword = %v, %v; want true", ok, err)
	}
	u, err := s.GetByID(ctx, userID)
	if err != nil || u.PasswordHash != "new" || u.SessionVersion != before+1 {
		t.Errorf("after change: hash=%q version=%d err=%v; want new and %d", u.PasswordHash, u.SessionVersion, err, before+1)
	}
	if ok, _ := s.ChangePassword(ctx, userID, "x", "again"); ok {
		t.Error("the replaced hash was accepted a second time")
	}
}

func TestUserStore_SSOLink(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	if p, id, err := s.SSOLink(ctx, userID); err != nil || p != "" || id != "" {
		t.Errorf("unlinked SSOLink = %q, %q, %v; want empty", p, id, err)
	}
	if err := s.LinkSSO(ctx, userID, "ldap", "uid_"+suffix); err != nil {
		t.Fatal(err)
	}
	if p, id, err := s.SSOLink(ctx, userID); err != nil || p != "ldap" || id != "uid_"+suffix {
		t.Errorf("SSOLink = %q, %q, %v", p, id, err)
	}
	other := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	if err := s.LinkSSO(ctx, other, "ldap", "uid_"+suffix); err == nil {
		t.Error("linking an SSO identity already used by another account must fail")
	}
	if _, _, err := s.SSOLink(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user = %v; want sql.ErrNoRows", err)
	}
}

func TestUserStore_TOTP(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	if err := s.SetTOTPSecret(ctx, userID, "SECRET"); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetByIDWithTOTP(ctx, userID)
	if err != nil || u.TOTPSecret.String != "SECRET" || u.TOTPEnabled {
		t.Fatalf("after SetTOTPSecret: %+v, %v", u, err)
	}
	if err := s.SetTOTPEnabled(ctx, userID, true, "SECRET2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBackupCodes(ctx, userID, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	u, err = s.GetByIDWithTOTP(ctx, userID)
	if err != nil || !u.TOTPEnabled || u.TOTPSecret.String != "SECRET2" || !slices.Equal(u.TOTPBackupCodes, []string{"a", "b"}) {
		t.Fatalf("after enable: %+v, %v", u, err)
	}

	if err := s.SetTOTPEnabled(ctx, userID, false, "ignored"); err != nil {
		t.Fatal(err)
	}
	u, err = s.GetByIDWithTOTP(ctx, userID)
	if err != nil || u.TOTPEnabled || u.TOTPSecret.Valid || len(u.TOTPBackupCodes) != 0 {
		t.Errorf("disable must clear the secret and backup codes: %+v, %v", u, err)
	}

	if _, err := s.GetByIDWithTOTP(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user = %v; want sql.ErrNoRows", err)
	}
}

func TestUserStore_UpdateProfile(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	ev := store.NewEmailVerificationStore(db)
	suffix := testutil.UniqueSuffix(t)
	u := createUser(t, db, "prof_"+suffix, "prof_"+suffix+"@test.invalid")
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM email_verification_tokens WHERE lower(email) LIKE $1`, "%prof_"+suffix+"@test.invalid")
	})
	if _, err := s.MarkEmailVerified(ctx, u.ID, u.Email); err != nil {
		t.Fatal(err)
	}

	changed, err := s.UpdateProfile(ctx, u.ID, "Name", u.Email, "bio", "co", "loc")
	if err != nil || changed {
		t.Fatalf("same email: UpdateProfile = %v, %v; want unchanged", changed, err)
	}
	got, _ := s.GetByID(ctx, u.ID)
	if got.Name != "Name" || got.Bio != "bio" || got.Company != "co" || got.Location != "loc" || got.EmailVerifiedAt == nil {
		t.Errorf("profile = %+v; want fields saved and verification kept", got)
	}

	newEmail := "prof_new_" + suffix + "@test.invalid"
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NULL WHERE id = $1`, u.ID)
	if _, _, err := ev.Issue(ctx, u.ID, "tok_"+suffix, time.Hour, time.Minute); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, u.ID)

	changed, err = s.UpdateProfile(ctx, u.ID, "Name", newEmail, "bio", "co", "loc")
	if err != nil || !changed {
		t.Fatalf("new email: UpdateProfile = %v, %v; want changed", changed, err)
	}
	got, _ = s.GetByID(ctx, u.ID)
	if got.Email != newEmail || got.EmailVerifiedAt != nil {
		t.Errorf("email=%q verified=%v; want the new address, unverified", got.Email, got.EmailVerifiedAt)
	}
	if link, _ := ev.Lookup(ctx, "tok_"+suffix); link.State != model.EmailVerificationInvalid {
		t.Errorf("old link state = %s; want invalid", link.State)
	}

	other := createUser(t, db, "prof2_"+suffix, "prof2_"+suffix+"@test.invalid")
	if _, err := s.UpdateProfile(ctx, u.ID, "", other.Email, "", "", ""); err == nil {
		t.Error("taking another account's email must fail")
	}
	if _, err := s.UpdateProfile(ctx, -1, "", "x@test.invalid", "", "", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user = %v; want sql.ErrNoRows", err)
	}
}

func TestUserStore_DeleteByID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	u := createUser(t, db, "del_"+testutil.UniqueSuffix(t), "del_"+testutil.UniqueSuffix(t)+"@test.invalid")

	if err := s.DeleteByID(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByID(ctx, u.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetByID after delete = %v; want sql.ErrNoRows", err)
	}
	if err := s.DeleteByID(ctx, u.ID); err != nil {
		t.Errorf("deleting a missing user = %v; want nil", err)
	}
}

func TestUserStore_Preferences(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	light, dark, key, err := s.GetLayoutPrefs(ctx, userID)
	if err != nil || light != "github" || dark != "github-dark" || key != "" {
		t.Errorf("default GetLayoutPrefs = %q, %q, %q, %v", light, dark, key, err)
	}
	if err := s.UpdateCodeThemes(ctx, userID, "solarized-light", "dracula"); err != nil {
		t.Fatal(err)
	}
	if light, dark, _, _ = s.GetLayoutPrefs(ctx, userID); light != "solarized-light" || dark != "dracula" {
		t.Errorf("themes = %q, %q", light, dark)
	}
	if _, _, _, err := s.GetLayoutPrefs(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user = %v; want sql.ErrNoRows", err)
	}

	prefs := model.NotificationPrefs{EmailNotifications: false, EmailDigest: "weekly", NotifyPRReview: false, NotifyMention: false}
	if err := s.UpdateNotificationPrefs(ctx, userID, prefs); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetByID(ctx, userID)
	if u.EmailNotifications || u.EmailDigest != "weekly" || u.NotifyPRReview || u.NotifyMention {
		t.Errorf("prefs = %+v", u)
	}
	if err := s.UpdateNotificationPrefs(ctx, userID, model.NotificationPrefs{EmailDigest: "hourly"}); err == nil {
		t.Error("an unknown digest mode must violate the check constraint")
	}

	if err := s.UpdateKeepEmailPrivate(ctx, userID, false); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetByID(ctx, userID); u.KeepEmailPrivate {
		t.Error("KeepEmailPrivate not cleared")
	}
	if err := s.UpdateKeepEmailPrivate(ctx, userID, true); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetByID(ctx, userID); !u.KeepEmailPrivate {
		t.Error("KeepEmailPrivate not set")
	}
}

func TestUserStore_ListUsersForDigest(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	daily := testutil.SeedUser(t, db, "d"+suffix)
	dailyOff := testutil.SeedUser(t, db, "o"+suffix)
	weekly := testutil.SeedUser(t, db, "w"+suffix)
	testutil.Exec(t, db, `UPDATE users SET email_digest = 'daily' WHERE id = ANY($1)`, []int64{daily, dailyOff})
	testutil.Exec(t, db, `UPDATE users SET email_notifications = FALSE WHERE id = $1`, dailyOff)
	testutil.Exec(t, db, `UPDATE users SET email_digest = 'weekly' WHERE id = $1`, weekly)

	users, err := s.ListUsersForDigest(ctx, "daily")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	if !slices.Contains(ids, daily) || slices.Contains(ids, dailyOff) || slices.Contains(ids, weekly) {
		t.Errorf("daily digest ids include daily=%v dailyOff=%v weekly=%v; want true false false",
			slices.Contains(ids, daily), slices.Contains(ids, dailyOff), slices.Contains(ids, weekly))
	}
}

func TestUserStore_BatchLookups(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	a := createUser(t, db, "batch_a_"+suffix, "batch_a_"+suffix+"@test.invalid")
	b := createUser(t, db, "batch_b_"+suffix, "batch_b_"+suffix+"@test.invalid")

	t.Run("empty input", func(t *testing.T) {
		if us, err := s.GetManyByUsernames(ctx, nil); err != nil || us != nil {
			t.Errorf("GetManyByUsernames(nil) = %v, %v", us, err)
		}
		if us, err := s.GetManyByIDs(ctx, nil); err != nil || us != nil {
			t.Errorf("GetManyByIDs(nil) = %v, %v", us, err)
		}
		if m, err := s.UsernamesByIDs(ctx, nil); err != nil || m == nil || len(m) != 0 {
			t.Errorf("UsernamesByIDs(nil) = %v, %v; want an empty non-nil map", m, err)
		}
		if m, err := s.AvatarKeysByOwnerName(ctx, nil); err != nil || m == nil || len(m) != 0 {
			t.Errorf("AvatarKeysByOwnerName(nil) = %v, %v; want an empty non-nil map", m, err)
		}
	})

	t.Run("missing rows are skipped", func(t *testing.T) {
		byName, err := s.GetManyByUsernames(ctx, []string{a.Username, b.Username, "missing_" + suffix})
		if err != nil {
			t.Fatal(err)
		}
		byID, err := s.GetManyByIDs(ctx, []int64{a.ID, b.ID, -1})
		if err != nil {
			t.Fatal(err)
		}
		for name, us := range map[string][]model.User{"GetManyByUsernames": byName, "GetManyByIDs": byID} {
			var ids []int64
			for _, u := range us {
				ids = append(ids, u.ID)
			}
			slices.Sort(ids)
			if !slices.Equal(ids, []int64{a.ID, b.ID}) {
				t.Errorf("%s ids = %v; want %v", name, ids, []int64{a.ID, b.ID})
			}
		}
		names, err := s.UsernamesByIDs(ctx, []int64{a.ID, b.ID, -1})
		if err != nil || len(names) != 2 || names[a.ID] != a.Username || names[b.ID] != b.Username {
			t.Errorf("UsernamesByIDs = %v, %v", names, err)
		}
	})
}

func TestUserStore_PinnedRepos(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	add := func(repoID int64, prune []int64, limit int) bool {
		t.Helper()
		ok, err := s.AddPinnedRepo(ctx, userID, repoID, prune, limit)
		if err != nil {
			t.Fatalf("AddPinnedRepo(%d): %v", repoID, err)
		}
		return ok
	}
	pins := func() []int64 {
		t.Helper()
		ids, err := s.GetPinnedRepoIDs(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}

	if ids := pins(); len(ids) != 0 {
		t.Fatalf("new user pins = %v; want none", ids)
	}
	if !add(10, nil, 3) || !add(20, nil, 3) || !add(30, nil, 3) {
		t.Fatal("pins under the limit refused")
	}
	if got := pins(); !slices.Equal(got, []int64{10, 20, 30}) {
		t.Fatalf("pins = %v; want insertion order [10 20 30]", got)
	}
	if add(40, nil, 3) {
		t.Error("a pin over the limit was added")
	}
	if !add(20, nil, 3) {
		t.Error("re-pinning an existing repo must succeed")
	}
	if got := pins(); !slices.Equal(got, []int64{10, 20, 30}) {
		t.Errorf("pins = %v; want unchanged", got)
	}
	if !add(40, []int64{10}, 3) {
		t.Error("pruning a stale pin must make room")
	}
	if got := pins(); !slices.Equal(got, []int64{20, 30, 40}) {
		t.Errorf("pins = %v; want [20 30 40]", got)
	}

	if err := s.RemovePinnedRepo(ctx, userID, 30); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePinnedRepo(ctx, userID, 999); err != nil {
		t.Fatal(err)
	}
	if got := pins(); !slices.Equal(got, []int64{20, 40}) {
		t.Errorf("pins = %v; want [20 40]", got)
	}

	if _, err := s.AddPinnedRepo(ctx, -1, 1, nil, 3); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user = %v; want sql.ErrNoRows", err)
	}
	if _, err := s.GetPinnedRepoIDs(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetPinnedRepoIDs unknown user = %v; want sql.ErrNoRows", err)
	}
}

func TestUserStore_Avatars(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix

	if old, err := s.SwapAvatarKey(ctx, userID, "k1"); err != nil || old != "" {
		t.Fatalf("first swap = %q, %v; want empty", old, err)
	}
	if old, err := s.SwapAvatarKey(ctx, userID, "k2"); err != nil || old != "k1" {
		t.Fatalf("second swap = %q, %v; want k1", old, err)
	}
	if _, err := s.SwapAvatarKey(ctx, -1, "k"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user = %v; want sql.ErrNoRows", err)
	}

	org := &model.Organization{Name: "avatarorg_" + suffix}
	if err := store.NewOrgStore(db).Create(ctx, org); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	testutil.Exec(t, db, `UPDATE organizations SET avatar_key = 'orgkey' WHERE id = $1`, org.ID)
	noAvatar := testutil.SeedUser(t, db, "n"+suffix)

	keys, err := s.AvatarKeysByOwnerName(ctx, []string{username, org.Name, "testuser_n" + suffix, "missing_" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[username] != "k2" || keys[org.Name] != "orgkey" {
		t.Errorf("AvatarKeysByOwnerName = %v (noAvatar user %d must be absent)", keys, noAvatar)
	}
}

func TestUserStore_IsSoleOrgOwner(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewUserStore(db)
	orgs := store.NewOrgStore(db)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, "o"+suffix)
	coOwner := testutil.SeedUser(t, db, "c"+suffix)
	member := testutil.SeedUser(t, db, "m"+suffix)

	org := &model.Organization{Name: "soleorg_" + suffix}
	if err := orgs.Create(ctx, org); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	for id, role := range map[int64]model.OrgRole{owner: model.OrgRoleOwner, member: model.OrgRoleMember} {
		if err := orgs.AddMember(ctx, org.ID, id, role); err != nil {
			t.Fatal(err)
		}
	}

	check := func(name string, id int64, want bool) {
		t.Helper()
		if got, err := s.IsSoleOrgOwner(ctx, id); err != nil || got != want {
			t.Errorf("%s: IsSoleOrgOwner = %v, %v; want %v", name, got, err, want)
		}
	}
	check("lone owner", owner, true)
	check("plain member", member, false)
	check("no orgs", coOwner, false)
	if err := orgs.AddMember(ctx, org.ID, coOwner, model.OrgRoleOwner); err != nil {
		t.Fatal(err)
	}
	check("owner with a co-owner", owner, false)
}

func TestUserStore_DeleteWithOwnedRepos(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes the user and their repos and hands their other content to the ghost", func(t *testing.T) {
		db := testutil.OpenFreshTestDB(t)
		s := store.NewUserStore(db)
		discussions := store.NewDiscussionStore(db)
		leaver := testutil.SeedUser(t, db, "leaver")
		other := testutil.SeedUser(t, db, "other")
		ownRepo := testutil.SeedRepo(t, db, leaver, "testuser_leaver", "own")
		elsewhere := testutil.SeedRepo(t, db, other, "testuser_other", "else")
		testutil.Exec(t, db, `UPDATE users SET avatar_key = 'leaver-key' WHERE id = $1`, leaver)
		cats, err := discussions.ListCategories(ctx)
		if err != nil || len(cats) == 0 {
			t.Fatalf("categories: %v, %v", cats, err)
		}
		d := &model.Discussion{RepoID: elsewhere, CategoryID: cats[0].ID, Title: "t", AuthorID: leaver, AuthorName: "testuser_leaver"}
		if err := discussions.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
		reply := &model.DiscussionReply{DiscussionID: d.ID, AuthorID: leaver, AuthorName: "testuser_leaver", Body: "b"}
		if err := discussions.CreateReply(ctx, reply); err != nil {
			t.Fatal(err)
		}

		key, err := s.DeleteWithOwnedRepos(ctx, leaver, []int64{ownRepo})
		if err != nil {
			t.Fatalf("DeleteWithOwnedRepos: %v", err)
		}
		if key != "leaver-key" {
			t.Errorf("avatar key = %q; want leaver-key", key)
		}
		if _, err := s.GetByID(ctx, leaver); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("user still exists: %v", err)
		}
		var repos int
		if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE id = $1`, ownRepo).Scan(&repos); err != nil || repos != 0 {
			t.Errorf("own repo count = %d, %v; want 0", repos, err)
		}
		var ghostID int64
		var ghostName string
		if err := db.QueryRow(`SELECT id, username FROM users WHERE id = ghost_user_id()`).Scan(&ghostID, &ghostName); err != nil {
			t.Fatal(err)
		}
		got, err := discussions.GetByNumber(ctx, elsewhere, d.Number)
		if err != nil || got == nil || got.AuthorID != ghostID || got.AuthorName != ghostName {
			t.Errorf("discussion = %+v, %v; want authored by the ghost %d %q", got, err, ghostID, ghostName)
		}
		rs, err := discussions.ListReplies(ctx, d.ID)
		if err != nil || len(rs) != 1 || rs[0].AuthorID != ghostID || rs[0].AuthorName != ghostName {
			t.Errorf("replies = %+v, %v; want authored by the ghost", rs, err)
		}
	})

	t.Run("a changed set of repos aborts the delete", func(t *testing.T) {
		db := testutil.OpenFreshTestDB(t)
		s := store.NewUserStore(db)
		userID := testutil.SeedUser(t, db, "u")
		repoID := testutil.SeedRepo(t, db, userID, "testuser_u", "r")

		for name, ids := range map[string][]int64{"repo missing from the list": nil, "extra repo in the list": {repoID, repoID + 1000}} {
			if _, err := s.DeleteWithOwnedRepos(ctx, userID, ids); !errors.Is(err, store.ErrOwnedReposChanged) {
				t.Errorf("%s: err = %v; want ErrOwnedReposChanged", name, err)
			}
		}
		if _, err := s.GetByID(ctx, userID); err != nil {
			t.Errorf("user deleted despite the abort: %v", err)
		}
		var repos int
		if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE id = $1`, repoID).Scan(&repos); err != nil || repos != 1 {
			t.Errorf("repo count = %d, %v; want it kept", repos, err)
		}
	})

	t.Run("a soft-deleted repo is not in the live set", func(t *testing.T) {
		db := testutil.OpenFreshTestDB(t)
		s := store.NewUserStore(db)
		userID := testutil.SeedUser(t, db, "u")
		repoID := testutil.SeedRepo(t, db, userID, "testuser_u", "r")
		testutil.Exec(t, db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, repoID)
		if _, err := s.DeleteWithOwnedRepos(ctx, userID, nil); err != nil {
			t.Fatalf("DeleteWithOwnedRepos: %v", err)
		}
		var repos int
		if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE id = $1`, repoID).Scan(&repos); err != nil || repos != 0 {
			t.Errorf("soft-deleted repo count = %d, %v; want 0 (removed with the owner)", repos, err)
		}
	})

	t.Run("the sole owner of an org cannot be deleted", func(t *testing.T) {
		db := testutil.OpenFreshTestDB(t)
		s := store.NewUserStore(db)
		orgs := store.NewOrgStore(db)
		userID := testutil.SeedUser(t, db, "u")
		org := &model.Organization{Name: "soleorg"}
		if err := orgs.Create(ctx, org); err != nil {
			t.Fatal(err)
		}
		if err := orgs.AddMember(ctx, org.ID, userID, model.OrgRoleOwner); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DeleteWithOwnedRepos(ctx, userID, nil); !errors.Is(err, store.ErrLastOrgOwner) {
			t.Errorf("err = %v; want ErrLastOrgOwner", err)
		}
		if _, err := s.GetByID(ctx, userID); err != nil {
			t.Errorf("user deleted despite the abort: %v", err)
		}
	})

	t.Run("the last active superadmin cannot be deleted", func(t *testing.T) {
		db := testutil.OpenFreshTestDB(t)
		s := store.NewUserStore(db)
		admin := testutil.SeedSuperadmin(t, db, "a")
		if _, err := s.DeleteWithOwnedRepos(ctx, admin, nil); !errors.Is(err, store.ErrLastSuperadmin) {
			t.Errorf("err = %v; want ErrLastSuperadmin", err)
		}
		second := testutil.SeedSuperadmin(t, db, "b")
		if _, err := s.DeleteWithOwnedRepos(ctx, admin, nil); err != nil {
			t.Errorf("with another superadmin: err = %v; want nil", err)
		}
		if _, err := s.GetByID(ctx, second); err != nil {
			t.Errorf("the other superadmin is gone: %v", err)
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		db := testutil.OpenFreshTestDB(t)
		key, err := store.NewUserStore(db).DeleteWithOwnedRepos(ctx, -1, nil)
		if err != nil || key != "" {
			t.Errorf("DeleteWithOwnedRepos = %q, %v; want empty and nil", key, err)
		}
	})
}

func TestUserStore_ClosedDBErrors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := store.NewUserStore(db)
	u := &model.User{Username: "x", Email: "x@test.invalid"}

	checks := map[string]error{}
	checks["Create"] = s.Create(ctx, u)
	_, checks["OwnerNameTaken"] = s.OwnerNameTaken(ctx, "x")
	_, checks["GetByOAuthID"] = s.GetByOAuthID(ctx, "p", "i")
	_, checks["CreateOAuthUser"] = s.CreateOAuthUser(ctx, "x", "x", "p", "i", "", false)
	_, checks["LinkOAuthByVerifiedEmail"] = s.LinkOAuthByVerifiedEmail(ctx, 1, "x", "p", "i")
	_, checks["ClaimReauthAttempt"] = s.ClaimReauthAttempt(ctx, 1, 1, time.Hour)
	checks["ReleaseReauthAttempt"] = s.ReleaseReauthAttempt(ctx, 1)
	_, checks["IssueReauthCode"] = s.IssueReauthCode(ctx, 1, "h", time.Hour, time.Hour, 1, time.Hour)
	checks["StoreReauthCode"] = s.StoreReauthCode(ctx, 1, "h", time.Hour)
	_, _, checks["SSOLink"] = s.SSOLink(ctx, 1)
	_, checks["LiveReauthCode"] = s.LiveReauthCode(ctx, 1)
	_, checks["SpendReauthCode"] = s.SpendReauthCode(ctx, 1, "h")
	_, checks["ChangePassword"] = s.ChangePassword(ctx, 1, "a", "b")
	_, checks["BumpSessionVersion"] = s.BumpSessionVersion(ctx, 1)
	_, checks["SessionState"] = s.SessionState(ctx, 1)
	_, checks["SessionVersion"] = s.SessionVersion(ctx, 1)
	_, checks["MarkEmailVerified"] = s.MarkEmailVerified(ctx, 1, "x")
	_, checks["LinkOAuth"] = s.LinkOAuth(ctx, 1, "p", "i")
	_, checks["UnlinkOAuth"] = s.UnlinkOAuth(ctx, 1, "p")
	_, checks["CountAccounts"] = s.CountAccounts(ctx)
	_, checks["CreateSuperadmin"] = s.CreateSuperadmin(ctx, "x", "x", "h")
	checks["LinkSSO"] = s.LinkSSO(ctx, 1, "p", "i")
	_, checks["GetByEmailWithTOTP"] = s.GetByEmailWithTOTP(ctx, "x")
	checks["SetTOTPSecret"] = s.SetTOTPSecret(ctx, 1, "s")
	checks["SetTOTPEnabled on"] = s.SetTOTPEnabled(ctx, 1, true, "s")
	checks["SetTOTPEnabled off"] = s.SetTOTPEnabled(ctx, 1, false, "")
	checks["SetBackupCodes"] = s.SetBackupCodes(ctx, 1, nil)
	_, checks["UpdateProfile"] = s.UpdateProfile(ctx, 1, "", "x", "", "", "")
	checks["DeleteByID"] = s.DeleteByID(ctx, 1)
	_, checks["IsSoleOrgOwner"] = s.IsSoleOrgOwner(ctx, 1)
	_, checks["DeleteWithOwnedRepos"] = s.DeleteWithOwnedRepos(ctx, 1, nil)
	checks["UpdateNotificationPrefs"] = s.UpdateNotificationPrefs(ctx, 1, model.NotificationPrefs{})
	checks["UpdateCodeThemes"] = s.UpdateCodeThemes(ctx, 1, "a", "b")
	_, _, _, checks["GetLayoutPrefs"] = s.GetLayoutPrefs(ctx, 1)
	checks["UpdateKeepEmailPrivate"] = s.UpdateKeepEmailPrivate(ctx, 1, true)
	_, checks["ListUsersForDigest"] = s.ListUsersForDigest(ctx, "daily")
	_, checks["GetManyByUsernames"] = s.GetManyByUsernames(ctx, []string{"x"})
	_, checks["UsernamesByIDs"] = s.UsernamesByIDs(ctx, []int64{1})
	_, checks["GetManyByIDs"] = s.GetManyByIDs(ctx, []int64{1})
	_, checks["AddPinnedRepo"] = s.AddPinnedRepo(ctx, 1, 1, nil, 1)
	checks["RemovePinnedRepo"] = s.RemovePinnedRepo(ctx, 1, 1)
	_, checks["GetPinnedRepoIDs"] = s.GetPinnedRepoIDs(ctx, 1)
	_, checks["SwapAvatarKey"] = s.SwapAvatarKey(ctx, 1, "k")
	_, checks["AvatarKeysByOwnerName"] = s.AvatarKeysByOwnerName(ctx, []string{"x"})
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s on a closed db must fail", name)
		}
	}
}

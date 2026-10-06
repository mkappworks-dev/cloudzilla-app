package service_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newAdminUserServices(t *testing.T, db *sql.DB) *service.Services {
	t.Helper()
	return service.New(store.New(db), &config.Config{
		Auth: config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!", JWTExpiry: time.Hour},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	})
}

func usernameOf(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestAdminUserService_RefusesOwnAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	admin := newAdminUserServices(t, db).AdminUser
	me := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	name := usernameOf(t, db, me)

	actions := map[string]func() error{
		"suspend":    func() error { _, err := admin.Suspend(ctx, me, name); return err },
		"unsuspend":  func() error { _, err := admin.Unsuspend(ctx, me, name); return err },
		"promote":    func() error { _, err := admin.Promote(ctx, me, name); return err },
		"demote":     func() error { _, err := admin.Demote(ctx, me, name); return err },
		"reset 2fa":  func() error { _, err := admin.ResetTOTP(ctx, me, name); return err },
		"revoke":     func() error { _, _, err := admin.RevokeCredentials(ctx, me, name); return err },
		"delete":     func() error { _, err := admin.Delete(ctx, me, name, name); return err },
		"reset link": func() error { _, _, err := admin.IssuePasswordResetLink(ctx, me, name); return err },
	}
	for action, do := range actions {
		if err := do(); !errors.Is(err, service.ErrAdminSelf) {
			t.Errorf("%s self: got %v, want ErrAdminSelf", action, err)
		}
	}
}

func TestAdminUserService_LastActiveSuperadmin(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	ctx := context.Background()
	svcs := newAdminUserServices(t, db)
	admin := testutil.SeedSuperadmin(t, db, "admin")
	other := testutil.SeedSuperadmin(t, db, "other")
	otherName := usernameOf(t, db, other)

	if _, err := svcs.AdminUser.Suspend(ctx, admin, otherName); err != nil {
		t.Fatalf("suspend other: %v", err)
	}
	// Now admin is the only active superadmin and can't delete their own account.
	if err := svcs.User.DeleteUser(ctx, admin); !errors.Is(err, service.ErrLastSuperadmin) {
		t.Errorf("self-service delete of last superadmin: got %v", err)
	}
	if _, err := svcs.AdminUser.Promote(ctx, admin, otherName); !errors.Is(err, service.ErrUserSuspended) {
		t.Errorf("promote suspended: got %v", err)
	}
	if _, err := svcs.AdminUser.Unsuspend(ctx, admin, otherName); err != nil {
		t.Fatalf("unsuspend: %v", err)
	}
	if _, err := svcs.AdminUser.Demote(ctx, admin, otherName); err != nil {
		t.Fatalf("demote other: %v", err)
	}
	if st, err := svcs.User.SessionState(ctx, other); err != nil || st.IsSuperadmin {
		t.Errorf("after demote: %+v, %v", st, err)
	}
}

func TestAdminUserService_ResetTOTP(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	svcs := newAdminUserServices(t, db)
	admin := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	testutil.EnableTOTP(t, db, userID)
	testutil.Exec(t, db, `UPDATE users SET totp_backup_codes = '{a,b}' WHERE id = $1`, userID)

	if _, err := svcs.AdminUser.ResetTOTP(ctx, admin, usernameOf(t, db, userID)); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var secret, codes sql.NullString
	if err := db.QueryRow(`SELECT totp_enabled, totp_secret, totp_backup_codes::text FROM users WHERE id = $1`, userID).Scan(&enabled, &secret, &codes); err != nil {
		t.Fatal(err)
	}
	if enabled || secret.Valid || codes.Valid {
		t.Errorf("after reset: enabled %v, secret %v, codes %v", enabled, secret, codes)
	}
}

func TestAdminUserService_ResetTOTPOffline(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	admin := newAdminUserServices(t, db).AdminUser
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	name := usernameOf(t, db, userID)
	testutil.EnableTOTP(t, db, userID)
	testutil.Exec(t, db, `UPDATE users SET totp_backup_codes = '{a,b}' WHERE id = $1`, userID)

	u, changed, err := admin.ResetTOTPOffline(ctx, name)
	if err != nil || !changed || u.ID != userID {
		t.Fatalf("ResetTOTPOffline = %+v, %v, %v; want user %d changed", u, changed, err, userID)
	}
	var enabled bool
	var secret, codes sql.NullString
	if err := db.QueryRow(`SELECT totp_enabled, totp_secret, totp_backup_codes::text FROM users WHERE id = $1`, userID).Scan(&enabled, &secret, &codes); err != nil {
		t.Fatal(err)
	}
	if enabled || secret.Valid || codes.Valid {
		t.Errorf("after reset: enabled %v, secret %v, codes %v", enabled, secret, codes)
	}

	if _, changed, err := admin.ResetTOTPOffline(ctx, name); err != nil || changed {
		t.Errorf("second reset = changed %v, %v; want unchanged", changed, err)
	}
	if _, _, err := admin.ResetTOTPOffline(ctx, "nobody_"+testutil.UniqueSuffix(t)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user err = %v; want sql.ErrNoRows", err)
	}
}

func TestAdminUserService_Delete(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	svcs := newAdminUserServices(t, db)
	admin := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	sfx := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, "owner_"+sfx)
	ownerName := usernameOf(t, db, owner)
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, "testorg_"+sfx).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, orgID, owner)

	if _, err := svcs.AdminUser.Delete(ctx, admin, ownerName, "not-the-name"); !errors.Is(err, service.ErrDeleteConfirmMismatch) {
		t.Errorf("mistyped confirmation: got %v", err)
	}
	if _, err := svcs.AdminUser.Delete(ctx, admin, ownerName, ownerName); !errors.Is(err, service.ErrSoleOrgOwner) {
		t.Errorf("sole org owner: got %v", err)
	}
	detail, err := svcs.AdminUser.Get(ctx, ownerName)
	if err != nil || !slices.Equal(detail.SoleOwnedOrgs, []string{"testorg_" + sfx}) {
		t.Errorf("Get sole owned orgs = %v, %v", detail, err)
	}

	plain := testutil.SeedUser(t, db, "plain_"+sfx)
	plainName := usernameOf(t, db, plain)
	if _, err := svcs.AdminUser.Delete(ctx, admin, plainName, plainName); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svcs.AdminUser.Get(ctx, plainName); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Get after delete: %v", err)
	}
}

func TestAdminUserService_GetHidesGhost(t *testing.T) {
	db := testutil.OpenTestDB(t)
	var ghost string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = ghost_user_id()`).Scan(&ghost); err != nil {
		t.Fatal(err)
	}
	if _, err := newAdminUserServices(t, db).AdminUser.Get(context.Background(), ghost); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Get ghost: %v", err)
	}
}

func TestAdminUserService_GetLastSignIn(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	id := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	testutil.Exec(t, db, `INSERT INTO audit_log (actor_id, actor_name, action, target_type, created_at) VALUES ($1, 'x', $2, 'user', NOW() - interval '1 day')`, id, model.AuditActionLogin)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM audit_log WHERE actor_id = $1`, id) })

	d, err := newAdminUserServices(t, db).AdminUser.Get(ctx, usernameOf(t, db, id))
	if err != nil || d.LastSignIn == nil || time.Since(*d.LastSignIn) < 23*time.Hour {
		t.Errorf("LastSignIn = %v, %v", d, err)
	}
}

func TestAdminUserService_IssuePasswordResetLink(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	me := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)

	u, link, err := svc.AdminUser.IssuePasswordResetLink(ctx, me, usernameOf(t, db, userID))
	if err != nil {
		t.Fatalf("IssuePasswordResetLink: %v", err)
	}
	if u.ID != userID {
		t.Errorf("user = %d, want %d", u.ID, userID)
	}
	token := link[strings.LastIndex(link, "/")+1:]
	got, err := svc.PasswordReset.Check(ctx, token)
	if err != nil || got.State != model.PasswordResetPending || got.UserID != userID || got.IssuedBy != model.PasswordResetByAdmin {
		t.Fatalf("Check = %+v, %v; want a pending admin link for user %d", got, err, userID)
	}

	box.Empty(t, 300*time.Millisecond)
	svc.AdminUser.NotifyPasswordResetLink(userID)
	notice := box.NextTo(t, email)
	if !strings.Contains(notice.Data, "administrator") || strings.Contains(notice.Data, token) {
		t.Errorf("notice = %.500s, want an administrator notice without the link", notice.Data)
	}
}

func TestAdminUserService_IssuePasswordResetLinkRefusals(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	me := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	suspended, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, suspended)
	suffix := testutil.UniqueSuffix(t)
	passwordless := testutil.SeedPasswordlessUser(t, db, suffix, "g-"+suffix)

	for _, tc := range []struct {
		name     string
		username string
		want     error
	}{
		{"suspended", usernameOf(t, db, suspended), service.ErrUserSuspended},
		{"passwordless", usernameOf(t, db, passwordless), service.ErrPasswordResetNoPassword},
		{"unknown", "nobody_" + suffix, sql.ErrNoRows},
	} {
		if _, _, err := svc.AdminUser.IssuePasswordResetLink(ctx, me, tc.username); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id IN ($1, $2)`, suspended, passwordless).Scan(&n); err != nil || n != 0 {
		t.Errorf("password_reset_tokens rows = %d, %v; want none", n, err)
	}
}

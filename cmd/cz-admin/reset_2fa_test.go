package main

// Integration tests for the reset-2fa command. They require TEST_DATABASE_DSN and skip otherwise.

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestReset2FA(t *testing.T) {
	db := testutil.OpenTestDB(t)
	stores := store.New(db)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	username := "testpw_" + suffix
	testutil.EnableTOTP(t, db, userID)
	testutil.Exec(t, db, `UPDATE users SET totp_backup_codes = '{a,b}' WHERE id = $1`, userID)
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM audit_log WHERE action = $1 AND target_id = $2`, model.AuditActionAdminUser2FAReset, userID)
	})

	var out bytes.Buffer
	if err := reset2FA(context.Background(), stores, &config.Config{}, username, &out, &out); err != nil {
		t.Fatalf("reset2FA: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Turned off two-factor authentication for @"+username) || strings.Contains(got, "suspended") {
		t.Errorf("output = %q", got)
	}
	var enabled bool
	if err := db.QueryRow(`SELECT totp_enabled FROM users WHERE id = $1`, userID).Scan(&enabled); err != nil || enabled {
		t.Errorf("totp_enabled = %v, %v; want false", enabled, err)
	}
	var actorID *int64
	var actorName string
	if err := db.QueryRow(`SELECT actor_id, actor_name FROM audit_log WHERE action = $1 AND target_id = $2`,
		model.AuditActionAdminUser2FAReset, userID).Scan(&actorID, &actorName); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if actorID != nil || actorName != "cz-admin" {
		t.Errorf("audit row = actor %v %q; want no actor id, cz-admin", actorID, actorName)
	}

	out.Reset()
	if err := reset2FA(context.Background(), stores, &config.Config{}, username, &out, &out); err != nil {
		t.Fatalf("second reset2FA: %v", err)
	}
	if !strings.Contains(out.String(), "doesn't have two-factor authentication turned on") {
		t.Errorf("second output = %q", out.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = $1 AND target_id = $2`, model.AuditActionAdminUser2FAReset, userID).Scan(&n); err != nil || n != 1 {
		t.Errorf("audit rows = %d, %v; want 1", n, err)
	}
}

func TestReset2FA_Suspended(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.EnableTOTP(t, db, userID)
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM audit_log WHERE action = $1 AND target_id = $2`, model.AuditActionAdminUser2FAReset, userID)
	})

	var out bytes.Buffer
	if err := reset2FA(context.Background(), store.New(db), &config.Config{}, "testpw_"+suffix, &out, &out); err != nil {
		t.Fatalf("reset2FA: %v", err)
	}
	if !strings.Contains(out.String(), "still suspended") {
		t.Errorf("output = %q; want a note that the account is still suspended", out.String())
	}
}

func TestReset2FA_UnknownUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	var out bytes.Buffer
	var ghost string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = ghost_user_id()`).Scan(&ghost); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nobody_" + testutil.UniqueSuffix(t), ghost} {
		err := reset2FA(context.Background(), store.New(db), &config.Config{}, name, &out, &out)
		if err == nil || !strings.Contains(err.Error(), "no user named") {
			t.Errorf("%s: err = %v; want no user named", name, err)
		}
	}
	if out.Len() != 0 {
		t.Errorf("output = %q; want none", out.String())
	}
}

// A link issued before the reset still works, so the documented recovery path can run in either order.
func TestReset2FA_KeepsResetLink(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	stores := store.New(db)
	cfg := &config.Config{Server: config.ServerConfig{BaseURL: "https://cz.test/"}}
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.EnableTOTP(t, db, userID)
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM audit_log WHERE target_type = 'user' AND target_id = $1`, userID)
	})

	link, err := issuePasswordResetLink(ctx, stores, cfg, "testpw_"+suffix)
	if err != nil {
		t.Fatalf("issuePasswordResetLink: %v", err)
	}
	if err := reset2FA(ctx, stores, cfg, "testpw_"+suffix, io.Discard, io.Discard); err != nil {
		t.Fatalf("reset2FA: %v", err)
	}
	resets := service.NewPasswordResetService(stores.PasswordReset, stores.User, nil, service.NewEmailService(cfg.SMTP), cfg.Server.BaseURL)
	got, err := resets.Check(ctx, strings.TrimPrefix(link, "https://cz.test/auth/password/reset/"))
	if err != nil || got.State != model.PasswordResetPending {
		t.Errorf("link after reset-2fa = %+v, %v; want still pending", got, err)
	}
}

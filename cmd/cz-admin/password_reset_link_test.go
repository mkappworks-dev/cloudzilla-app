package main

// Integration tests for the password-reset-link command. They require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestPasswordResetLink(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	stores := store.New(db)
	cfg := &config.Config{Server: config.ServerConfig{BaseURL: "https://cz.test/"}}
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")

	link, err := issuePasswordResetLink(ctx, stores, cfg, "testpw_"+suffix)
	if err != nil {
		t.Fatalf("issuePasswordResetLink: %v", err)
	}
	const prefix = "https://cz.test/auth/password/reset/"
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("link = %q, want it under %s", link, prefix)
	}
	resets := service.NewPasswordResetService(stores.PasswordReset, stores.User, nil, service.NewEmailService(cfg.SMTP), cfg.Server.BaseURL)
	got, err := resets.Check(ctx, strings.TrimPrefix(link, prefix))
	if err != nil || got.State != model.PasswordResetPending || got.UserID != userID || got.IssuedBy != model.PasswordResetByCLI {
		t.Errorf("Check = %+v, %v; want a pending CLI link for user %d", got, err, userID)
	}

	var actorID *int64
	var actorName, metadata string
	if err := db.QueryRow(`SELECT actor_id, actor_name, metadata FROM audit_log WHERE action = $1 AND target_id = $2`,
		model.AuditActionPasswordResetLink, userID).Scan(&actorID, &actorName, &metadata); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	testutil.Exec(t, db, `DELETE FROM audit_log WHERE action = $1 AND target_id = $2`, model.AuditActionPasswordResetLink, userID)
	if actorID != nil || actorName != "cz-admin" || !strings.Contains(metadata, `"issued_by": "cli"`) && !strings.Contains(metadata, `"issued_by":"cli"`) {
		t.Errorf("audit row = actor %v %q, metadata %s; want no actor id, cz-admin, issued_by cli", actorID, actorName, metadata)
	}
}

func TestPasswordResetLink_Refusals(t *testing.T) {
	db := testutil.OpenTestDB(t)
	stores := store.New(db)
	cfg := &config.Config{}
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g-"+suffix)

	if _, err := issuePasswordResetLink(context.Background(), stores, cfg, "nobody_"+suffix); err == nil || !strings.Contains(err.Error(), "no user named") {
		t.Errorf("unknown user err = %v", err)
	}
	if _, err := issuePasswordResetLink(context.Background(), stores, cfg, "testnopw_"+suffix); err == nil || !strings.Contains(err.Error(), "no password to reset") {
		t.Errorf("passwordless user err = %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = $1`, userID).Scan(&n); err != nil || n != 0 {
		t.Errorf("password_reset_tokens rows = %d, %v; want none", n, err)
	}
}

func TestPasswordResetLink_SuspendedUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)

	_, err := issuePasswordResetLink(context.Background(), store.New(db), &config.Config{}, "testpw_"+suffix)
	if err == nil || !strings.Contains(err.Error(), "unsuspend") {
		t.Errorf("suspended user err = %v, want an unsuspend hint", err)
	}
}

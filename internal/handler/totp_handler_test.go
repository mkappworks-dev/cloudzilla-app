package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestVerifyTOTP_BackupCodeStartsSession(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAuthHandler(db)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	testutil.EnableTOTP(t, db, userID)
	raw, hashes, err := h.Services.TOTP.GenerateBackupCodes()
	if err != nil {
		t.Fatalf("GenerateBackupCodes: %v", err)
	}
	if err := store.NewUserStore(db).SetBackupCodes(context.Background(), userID, hashes); err != nil {
		t.Fatalf("SetBackupCodes: %v", err)
	}
	pending, err := h.Services.TOTP.GeneratePendingToken(userID, testJWTSecret, nil)
	if err != nil {
		t.Fatalf("GeneratePendingToken: %v", err)
	}

	const next = "/settings"
	form := url.Values{"backup_code": {raw[3]}, "next": {next}}
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "cz_totp_pending", Value: pending})
	rr := httptest.NewRecorder()
	h.VerifyTOTP(rr, req)

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != next {
		t.Fatalf("want 303 to %q, got %d to %q", next, rr.Code, rr.Header().Get("Location"))
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == testCookieName && c.Value != "" {
			return
		}
	}
	t.Errorf("no %s session cookie was set", testCookieName)
}

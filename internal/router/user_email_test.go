package router_test

// Integration tests for additional email addresses through the real route table.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const additionalEmailSlot = "#additional-email-error"

var confirmedPassword = service.Confirmation{Password: "password1"}

func emailSettingsSection(t *testing.T, h http.Handler, session string) string {
	t.Helper()
	rr := serve(h, browserRequest(http.MethodGet, "/settings", session, nil))
	body := rr.Body.String()
	start := strings.Index(body, `<section id="email"`)
	if rr.Code != http.StatusOK || start < 0 {
		t.Fatalf("GET /settings: %d, email section found: %v", rr.Code, start >= 0)
	}
	return body[start : start+strings.Index(body[start:], "</section>")]
}

func TestAdditionalEmail_AddAndRemoveNeedThePassword(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	session := makeJWT(t, userID, "testpw_"+suffix)
	address := "extra_" + suffix + "@test.invalid"
	post := func(path string, form url.Values) *http.Response {
		return serve(h, htmxRequest(browserRequest(http.MethodPost, path, session, form))).Result()
	}
	count := func() int { return countRows(t, db, `SELECT COUNT(*) FROM user_emails WHERE user_id = $1`, userID) }

	form := url.Values{"email": {address}}
	for _, password := range []string{"", "wrong"} {
		resp := post("/settings/emails", withPassword(form, password))
		if resp.Header.Get("HX-Retarget") != additionalEmailSlot || count() != 0 {
			t.Fatalf("password %q: retarget %q, %d rows; want the error slot and no row", password, resp.Header.Get("HX-Retarget"), count())
		}
	}
	if resp := post("/settings/emails", withPassword(form, "password1")); resp.StatusCode != http.StatusNoContent || count() != 1 {
		t.Fatalf("confirmed add: %d, %d rows", resp.StatusCode, count())
	}
	box.NextTo(t, address)
	takeAudit(t, db, model.AuditActionEmailAdd, userID)

	rows, _ := svc.EmailVerifier.Addresses(ctx, userID)
	removePath := "/settings/emails/" + strconv.FormatInt(rows[0].ID, 10) + "/remove"
	for _, password := range []string{"", "wrong"} {
		post(removePath, withPassword(url.Values{}, password))
		if count() != 1 {
			t.Fatalf("password %q: an unconfirmed request removed the address", password)
		}
	}
	if resp := post(removePath, withPassword(url.Values{}, "password1")); resp.StatusCode != http.StatusNoContent || count() != 0 {
		t.Fatalf("confirmed remove: %d, %d rows", resp.StatusCode, count())
	}
	takeAudit(t, db, model.AuditActionEmailRemove, userID)
}

func TestAdditionalEmail_NoSMTPHidesTheForm(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	session := makeJWT(t, userID, "testpw_"+suffix)

	resp := serve(h, htmxRequest(browserRequest(http.MethodPost, "/settings/emails", session, url.Values{"email": {"extra_" + suffix + "@test.invalid"}, "password": {"password1"}})))
	if resp.Header().Get("HX-Retarget") != additionalEmailSlot || !strings.Contains(resp.Body.String(), "outgoing email") {
		t.Errorf("add without SMTP: retarget %q, body %q; want the unavailable message", resp.Header().Get("HX-Retarget"), resp.Body.String())
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM user_emails WHERE user_id = $1`, userID); n != 0 {
		t.Errorf("stored %d rows without a way to verify them", n)
	}
	got := emailSettingsSection(t, h, session)
	if strings.Contains(got, ">Add email<") || strings.Contains(got, `id="add-email-form"`) || !strings.Contains(got, "Adding another address needs outgoing email") {
		t.Errorf("without SMTP want the note and no form:\n%s", got)
	}
}

func TestAdditionalEmail_SettingsListsAddressesAndTheLinkVerifiesOne(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	session := makeJWT(t, userID, "testpw_"+suffix)
	address := "extra_" + suffix + "@test.invalid"

	if got := emailSettingsSection(t, h, session); !strings.Contains(got, ">Add email<") || !strings.Contains(got, `id="add-email-form"`) {
		t.Errorf("with SMTP want the add form:\n%s", got)
	}
	if err := svc.EmailVerifier.AddAddress(context.Background(), userID, address, confirmedPassword); err != nil {
		t.Fatal(err)
	}
	token := box.NextTo(t, address).VerificationToken(t)
	got := emailSettingsSection(t, h, session)
	if !strings.Contains(got, address) || !strings.Contains(got, "Remove") || strings.Count(got, ">Unverified<") != 2 {
		t.Errorf("want the pending address, unverified, with a remove control:\n%s", got)
	}

	// The link works without a session, like the primary address's.
	page := serve(h, browserRequest(http.MethodGet, "/verify-email?token="+url.QueryEscape(token), "", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), address) {
		t.Fatalf("GET verify link: %d, names the address: %v", page.Code, strings.Contains(page.Body.String(), address))
	}
	done := serve(h, browserRequest(http.MethodPost, "/verify-email", "", url.Values{"token": {token}}))
	if done.Code != http.StatusOK {
		t.Fatalf("POST verify: %d", done.Code)
	}
	takeAudit(t, db, model.AuditActionEmailVerify, userID)
	if got := emailSettingsSection(t, h, session); strings.Count(got, ">Unverified<") != 1 || strings.Count(got, ">Verified<") != 1 {
		t.Errorf("want the extra address verified:\n%s", got)
	}
	if again := serve(h, browserRequest(http.MethodPost, "/verify-email", "", url.Values{"token": {token}})); again.Code != http.StatusBadRequest {
		t.Errorf("a spent link: %d, want 400", again.Code)
	}
}

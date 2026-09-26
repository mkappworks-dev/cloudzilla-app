package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newInviteHandler(db *sql.DB) *handler.Handler {
	h := newAuthHandler(db)
	h.Services.Invitation = service.NewInvitationService(store.NewInvitationStore(db))
	return h
}

func inviteRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/invite/{token}", h.PageInvite)
	r.Post("/invite/{token}", h.PageInviteSubmit)
	return r
}

func inviteRequest(method, token string, form url.Values) *http.Request {
	if method == http.MethodGet {
		return httptest.NewRequest(method, "/invite/"+token, nil)
	}
	req := httptest.NewRequest(method, "/invite/"+token, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func serveInvite(db *sql.DB, method, token string, form url.Values) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	inviteRouter(newInviteHandler(db)).ServeHTTP(rr, inviteRequest(method, token, form))
	return rr
}

func markInvitationAccepted(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `UPDATE invitations SET accepted_at = NOW() WHERE id = $1`, id); err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
}

func invitationAccepted(t *testing.T, db *sql.DB, id int64) bool {
	t.Helper()
	var accepted bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT accepted_at IS NOT NULL FROM invitations WHERE id = $1`, id,
	).Scan(&accepted); err != nil {
		t.Fatalf("read invitation: %v", err)
	}
	return accepted
}

func assertInvalidInvitationPage(t *testing.T, body, hiddenEmail string) {
	t.Helper()
	if hiddenEmail != "" && strings.Contains(strings.ToLower(body), strings.ToLower(hiddenEmail)) {
		t.Errorf("unusable invitation must not reveal the invited email; body:\n%s", body)
	}
	if strings.Contains(body, `name="username"`) {
		t.Error("unusable invitation must not render the sign-up form")
	}
	if strings.Contains(body, "accepted") || strings.Contains(body, "expired") {
		t.Error("message must not say why the invitation is unusable")
	}
	if !strings.Contains(body, "no longer valid") {
		t.Errorf("want generic invalid-invitation message; body:\n%s", body)
	}
}

func TestPageInvite_UsableInvitation_PrefillsEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "invitee_" + testutil.UniqueSuffix(t) + "@test.invalid"
	_, token := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))

	body := serveInvite(db, http.MethodGet, token, nil).Body.String()

	if !strings.Contains(body, email) {
		t.Errorf("usable invitation must prefill the invitee email; body:\n%s", body)
	}
	if !strings.Contains(body, `name="username"`) {
		t.Error("usable invitation must render the sign-up form")
	}
}

// An unusable invite's email may belong to a registered account, so the
// unauthenticated link must not show it.
func TestPageInvite_UnusableInvitation_HidesEmail(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, db *sql.DB, suffix string) (token, email string)
	}{
		{"accepted", func(t *testing.T, db *sql.DB, suffix string) (string, string) {
			email := "invitee_" + suffix + "@test.invalid"
			id, token := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))
			markInvitationAccepted(t, db, id)
			return token, email
		}},
		{"expired", func(t *testing.T, db *sql.DB, suffix string) (string, string) {
			email := "invitee_" + suffix + "@test.invalid"
			_, token := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(-time.Hour))
			return token, email
		}},
		{"email already registered", func(t *testing.T, db *sql.DB, suffix string) (string, string) {
			testutil.SeedUser(t, db, suffix)
			email := "testuser_" + suffix + "@test.invalid"
			_, token := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))
			return token, email
		}},
		{"email registered in other case", func(t *testing.T, db *sql.DB, suffix string) (string, string) {
			testutil.SeedUser(t, db, suffix)
			email := "TestUser_" + suffix + "@test.invalid"
			_, token := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))
			return token, email
		}},
	}
	for _, tc := range cases {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				db := testutil.OpenTestDB(t)
				suffix := testutil.UniqueSuffix(t)
				token, email := tc.setup(t, db, suffix)

				form := url.Values{"username": {"invitee_" + suffix}, "password": {"password123"}}
				body := serveInvite(db, method, token, form).Body.String()

				assertInvalidInvitationPage(t, body, email)
			})
		}
	}
}

func TestPageInvite_UnknownToken_SameGenericPage(t *testing.T) {
	db := testutil.OpenTestDB(t)

	body := serveInvite(db, http.MethodGet, "no-such-token-"+testutil.UniqueSuffix(t), nil).Body.String()

	assertInvalidInvitationPage(t, body, "")
}

func TestPageInviteSubmit_Success_ClaimsInvitation(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "invitee_" + suffix + "@test.invalid"
	id, token := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	})

	form := url.Values{"username": {"invitee_" + suffix}, "password": {"password123"}}
	rr := serveInvite(db, http.MethodPost, token, form)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	if !invitationAccepted(t, db, id) {
		t.Error("a redeemed invitation must be marked accepted")
	}
	var invited bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT is_invited FROM users WHERE email = $1`, email,
	).Scan(&invited); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if !invited {
		t.Error("an account created from an invitation must be marked invited")
	}

	replay := serveInvite(db, http.MethodPost, token, url.Values{"username": {"replay_" + suffix}, "password": {"password123"}})
	assertInvalidInvitationPage(t, replay.Body.String(), email)
}

func TestPageInviteSubmit_UsernameTaken_FriendlyErrorAndInviteStaysUsable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	id, token := testutil.SeedInvitation(t, db, "invitee_"+suffix+"@test.invalid", time.Now().UTC().Add(time.Hour))

	form := url.Values{"username": {"testuser_" + suffix}, "password": {"password123"}}
	body := serveInvite(db, http.MethodPost, token, form).Body.String()

	assertNoDBErrorText(t, body)
	if !strings.Contains(body, "username is already taken") {
		t.Errorf("want username-taken message; body:\n%s", body)
	}
	if invitationAccepted(t, db, id) {
		t.Error("a failed account creation must leave the invitation usable")
	}
}

func TestPageInviteSubmit_ConcurrentSubmits_OneAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "invitee_" + suffix + "@test.invalid"
	_, token := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	})

	const submits = 5
	results := make(chan *httptest.ResponseRecorder, submits)
	var wg sync.WaitGroup
	for i := range submits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			form := url.Values{"username": {fmt.Sprintf("invitee_%s_%d", suffix, i)}, "password": {"password123"}}
			results <- serveInvite(db, http.MethodPost, token, form)
		}()
	}
	wg.Wait()
	close(results)

	redeemed := 0
	for rr := range results {
		if rr.Code == http.StatusSeeOther {
			redeemed++
			continue
		}
		assertInvalidInvitationPage(t, rr.Body.String(), "")
	}
	if redeemed != 1 {
		t.Errorf("want exactly 1 redeemed submit, got %d", redeemed)
	}
}

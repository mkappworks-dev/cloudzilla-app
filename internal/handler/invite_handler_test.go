package handler_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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

// seedInvitation inserts an invitation for email and returns its token. The
// invitation and its inviting superadmin are deleted when the test ends.
func seedInvitation(t *testing.T, db *sql.DB, email string, expiresAt time.Time, accepted bool) string {
	t.Helper()
	ctx := context.Background()
	adminID := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	s := store.NewInvitationStore(db)
	inv := &model.Invitation{
		Token:       "testinvite_" + testutil.UniqueSuffix(t),
		Email:       email,
		InvitedByID: adminID,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Create(ctx, inv); err != nil {
		t.Fatalf("seedInvitation: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM invitations WHERE id = $1`, inv.ID)
	})
	if accepted {
		if err := s.MarkAccepted(ctx, inv.ID); err != nil {
			t.Fatalf("seedInvitation accept: %v", err)
		}
	}
	return inv.Token
}

func inviteRequest(method, token string, form url.Values) *http.Request {
	if method == http.MethodGet {
		return httptest.NewRequest(method, "/invite/"+token, nil)
	}
	req := httptest.NewRequest(method, "/invite/"+token, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestPageInvite_UsableInvitation_PrefillsEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "invitee_" + testutil.UniqueSuffix(t) + "@test.invalid"
	token := seedInvitation(t, db, email, time.Now().UTC().Add(time.Hour), false)

	rr := httptest.NewRecorder()
	inviteRouter(newInviteHandler(db)).ServeHTTP(rr, inviteRequest(http.MethodGet, token, nil))

	body := rr.Body.String()
	if !strings.Contains(body, email) {
		t.Errorf("usable invitation must prefill the invitee email; body:\n%s", body)
	}
	if !strings.Contains(body, `name="username"`) {
		t.Error("usable invitation must render the sign-up form")
	}
}

// Once an invitation is accepted the account exists under the invited email,
// so an old invite URL must not reveal it.
func TestPageInvite_UnusableInvitation_HidesEmail(t *testing.T) {
	cases := []struct {
		name      string
		expiresAt time.Time
		accepted  bool
	}{
		{"accepted", time.Now().UTC().Add(time.Hour), true},
		{"expired", time.Now().UTC().Add(-time.Hour), false},
	}
	for _, tc := range cases {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				db := testutil.OpenTestDB(t)
				suffix := testutil.UniqueSuffix(t)
				email := "invitee_" + suffix + "@test.invalid"
				token := seedInvitation(t, db, email, tc.expiresAt, tc.accepted)

				form := url.Values{"username": {"invitee_" + suffix}, "password": {"password123"}}
				rr := httptest.NewRecorder()
				inviteRouter(newInviteHandler(db)).ServeHTTP(rr, inviteRequest(method, token, form))

				body := rr.Body.String()
				if strings.Contains(body, email) {
					t.Errorf("unusable invitation must not reveal the invited email; body:\n%s", body)
				}
				if strings.Contains(body, `name="username"`) {
					t.Error("unusable invitation must not render the sign-up form")
				}
				if strings.Contains(body, "accepted") || strings.Contains(body, "expired") {
					t.Error("message must not say whether the invitation was accepted or expired")
				}
				if !strings.Contains(body, "no longer valid") {
					t.Errorf("want generic invalid-invitation message; body:\n%s", body)
				}
			})
		}
	}
}

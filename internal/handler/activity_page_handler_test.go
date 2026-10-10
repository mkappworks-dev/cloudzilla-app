package handler_test

// Integration tests for the activity feed page. They require TEST_DATABASE_DSN and skip otherwise.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func activityRouter(h *handler.Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/activity", h.PageActivity)
	return middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r)
}

func getActivity(t *testing.T, h *handler.Handler, token, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	activityRouter(h).ServeHTTP(rr, req)
	return rr
}

func TestPageActivity_RedirectsAnonymousVisitorsToLogin(t *testing.T) {
	h := newPageHandler(t, testutil.OpenTestDB(t))

	rr := getActivity(t, h, "", "/activity")
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Errorf("response = %d to %q; want 303 to /login", rr.Code, rr.Header().Get("Location"))
	}
}

func TestPageActivity_ShowsAnEmptyStateWhenNothingHasHappened(t *testing.T) {
	db := testutil.OpenTestDB(t)
	user := seedSignedInUser(t, db)

	rr := getActivity(t, newPageHandler(t, db), user.token, "/activity")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}
	assertContains(t, rr.Body.String(), "No activity yet.")
}

func TestPageActivity_FallsBackForAnUnknownScopeAndABadPage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	user := seedSignedInUser(t, db)
	h := newPageHandler(t, db)
	allActive := regexp.MustCompile(`href="/activity"[^>]*aria-current="page"`)

	for _, target := range []string{
		"/activity?filter=bogus",
		"/activity?page=abc",
		"/activity?page=0",
		"/activity?page=-2",
	} {
		body := getActivity(t, h, user.token, target).Body.String()
		if !allActive.MatchString(body) {
			t.Errorf("%s: want the All activity tab selected", target)
		}
		if strings.Contains(body, "&larr; Previous</a>") {
			t.Errorf("%s: want page 1, with no previous link", target)
		}
	}
}

func TestPageActivity_SelectsTheRequestedScope(t *testing.T) {
	db := testutil.OpenTestDB(t)
	user := seedSignedInUser(t, db)
	h := newPageHandler(t, db)

	for filter, href := range map[string]string{"yours": "/activity?filter=yours", "watching": "/activity?filter=watching"} {
		body := getActivity(t, h, user.token, "/activity?filter="+filter).Body.String()
		if !regexp.MustCompile(`href="` + regexp.QuoteMeta(href) + `"[^>]*aria-current="page"`).MatchString(body) {
			t.Errorf("filter=%s: want that tab selected", filter)
		}
	}
}

func TestPageActivity_SaysSoWhenTheFeedCannotBeLoaded(t *testing.T) {
	h := newPageHandler(t, openSchemalessDB(t))

	rr := getActivity(t, h, makeIssueJWT(t, 1, "someone"), "/activity")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want the page to render anyway", rr.Code)
	}
	assertContains(t, rr.Body.String(), "We couldn&#39;t load your activity.")
}

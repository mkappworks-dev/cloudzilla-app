package router_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestLayout_CarriesTheViewersCodeThemes(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	rr := serve(h, browserRequest(http.MethodGet, "/explore", "", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /explore signed out = %d, want 200", rr.Code)
	}
	assertCodeThemes(t, rr.Body.String(), highlight.DefaultLight, highlight.DefaultDark)

	if err := svc.User.UpdateCodeThemes(context.Background(), userID, "solarized-light", "dracula"); err != nil {
		t.Fatalf("UpdateCodeThemes: %v", err)
	}
	rr = serve(h, browserRequest(http.MethodGet, "/explore", makeJWT(t, userID, "testuser_"+suffix), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /explore signed in = %d, want 200", rr.Code)
	}
	assertCodeThemes(t, rr.Body.String(), "solarized-light", "dracula")
	if !strings.Contains(rr.Body.String(), `href="/static/code-themes.css`) {
		t.Error("layout does not link code-themes.css")
	}
}

func assertCodeThemes(t *testing.T, body, light, dark string) {
	t.Helper()
	want := `data-code-light="` + light + `" data-code-dark="` + dark + `"`
	if !strings.Contains(body, want) {
		t.Errorf("<html> lacks %s", want)
	}
}

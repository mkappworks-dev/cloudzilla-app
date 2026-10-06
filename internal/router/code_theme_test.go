package router_test

import (
	"context"
	"net/http"
	"net/url"
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

func TestAppearanceSettings_SavesAndShowsTheThemes(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	session := makeJWT(t, userID, "testuser_"+suffix)

	req := browserRequest(http.MethodPost, "/settings/appearance", session,
		url.Values{"code_theme_light": {"gruvbox-light"}, "code_theme_dark": {"nord"}})
	req.Header.Set("HX-Request", "true")
	if rr := serve(h, req); rr.Code != http.StatusNoContent {
		t.Fatalf("POST /settings/appearance = %d, want 204", rr.Code)
	}
	light, dark, err := svc.User.CodeThemes(context.Background(), userID)
	if err != nil {
		t.Fatalf("CodeThemes: %v", err)
	}
	if light != "gruvbox-light" || dark != "nord" {
		t.Errorf("saved %q/%q, want gruvbox-light/nord", light, dark)
	}

	rr := serve(h, browserRequest(http.MethodGet, "/settings", session, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /settings = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`id="appearance"`,
		`name="code_theme_light" value="gruvbox-light"`,
		`name="code_theme_dark" value="nord"`,
		`data-value="plain"`,
		`class="hl-`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
}

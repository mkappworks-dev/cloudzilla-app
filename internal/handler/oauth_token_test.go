package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// newClosedDBTokenEndpoint serves /oauth/token over a closed database, so every
// store call fails.
func newClosedDBTokenEndpoint(t *testing.T) http.Handler {
	t.Helper()
	db, err := sql.Open("pgx", "postgres://unused")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.Close()
	svc := &service.Services{
		OAuthApp: service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), nil),
	}
	return http.HandlerFunc(handler.New(svc, &config.Config{}).TokenEndpoint)
}

func postToken(h http.Handler, form url.Values, basicUser, basicPass string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestTokenEndpoint_MalformedRequest_RFC6749Errors(t *testing.T) {
	h := newClosedDBTokenEndpoint(t)

	tests := []struct {
		name      string
		form      url.Values
		basicUser string
		want      string
	}{
		{"missing grant_type", url.Values{"code": {"c"}, "client_id": {"id"}, "client_secret": {"s"}}, "", "invalid_request"},
		{"unsupported grant_type", url.Values{"grant_type": {"password"}, "client_id": {"id"}, "client_secret": {"s"}}, "", "unsupported_grant_type"},
		{"missing code", url.Values{"grant_type": {"authorization_code"}, "client_id": {"id"}, "client_secret": {"s"}}, "", "invalid_request"},
		{"secret in both Basic and body", url.Values{"grant_type": {"authorization_code"}, "code": {"c"}, "client_secret": {"s"}}, "id", "invalid_request"},
		{"body client_id disagrees with Basic", url.Values{"grant_type": {"authorization_code"}, "code": {"c"}, "client_id": {"other"}}, "id", "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := postToken(h, tt.form, tt.basicUser, "s")
			if want := `{"error":"` + tt.want + `"}`; rr.Code != http.StatusBadRequest || strings.TrimSpace(rr.Body.String()) != want {
				t.Errorf("want 400 %s, got %d: %s", want, rr.Code, rr.Body.String())
			}
		})
	}
}

// A store failure is logged server-side; the client learns only that it happened.
func TestTokenEndpoint_StoreError_OpaqueServerError(t *testing.T) {
	rr := postToken(newClosedDBTokenEndpoint(t), url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"c"},
		"redirect_uri":  {"https://client.example/cb"},
		"client_id":     {"id"},
		"client_secret": {"s"},
	}, "", "")
	if want := `{"error":"server_error"}`; rr.Code != http.StatusInternalServerError || strings.TrimSpace(rr.Body.String()) != want {
		t.Errorf("want 500 %s, got %d: %s", want, rr.Code, rr.Body.String())
	}
}

// Every token-endpoint response opts out of caching, so the header can't depend on
// which branch answered.
func TestTokenEndpoint_ErrorResponses_NoStore(t *testing.T) {
	h := newClosedDBTokenEndpoint(t)
	for _, form := range []url.Values{
		{"grant_type": {"password"}},
		{"grant_type": {"authorization_code"}, "code": {"c"}, "client_id": {"id"}, "client_secret": {"s"}},
	} {
		if got := postToken(h, form, "", "").Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("grant_type %q: Cache-Control = %q, want no-store", form.Get("grant_type"), got)
		}
	}
}

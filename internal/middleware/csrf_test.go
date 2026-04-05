package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRF(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrf := CSRF(false)(okHandler)

	t.Run("GET requests pass through", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		csrf.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET returned %d, want 200", rec.Code)
		}
	})

	t.Run("POST without token returns 403", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/foo", nil)
		rec := httptest.NewRecorder()
		csrf.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST without token returned %d, want 403", rec.Code)
		}
	})

	t.Run("POST with matching header token succeeds", func(t *testing.T) {
		// First, do a GET to get the cookie
		getReq := httptest.NewRequest("GET", "/", nil)
		getRec := httptest.NewRecorder()
		csrf.ServeHTTP(getRec, getReq)

		cookies := getRec.Result().Cookies()
		var csrfToken string
		for _, c := range cookies {
			if c.Name == "csrf_token" {
				csrfToken = c.Value
				break
			}
		}
		if csrfToken == "" {
			t.Fatal("no csrf_token cookie set")
		}

		// Now POST with the token
		postReq := httptest.NewRequest("POST", "/api/foo", nil)
		postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
		postReq.Header.Set("X-CSRF-Token", csrfToken)
		postRec := httptest.NewRecorder()
		csrf.ServeHTTP(postRec, postReq)
		if postRec.Code != http.StatusOK {
			t.Errorf("POST with valid token returned %d, want 200", postRec.Code)
		}
	})

	t.Run("POST with matching form field succeeds", func(t *testing.T) {
		getReq := httptest.NewRequest("GET", "/", nil)
		getRec := httptest.NewRecorder()
		csrf.ServeHTTP(getRec, getReq)

		var csrfToken string
		for _, c := range getRec.Result().Cookies() {
			if c.Name == "csrf_token" {
				csrfToken = c.Value
				break
			}
		}

		postReq := httptest.NewRequest("POST", "/api/foo", strings.NewReader("csrf_token="+csrfToken))
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
		postRec := httptest.NewRecorder()
		csrf.ServeHTTP(postRec, postReq)
		if postRec.Code != http.StatusOK {
			t.Errorf("POST with form token returned %d, want 200", postRec.Code)
		}
	})

	t.Run("POST with mismatched token returns 403", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/foo", nil)
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "real-token"})
		req.Header.Set("X-CSRF-Token", "wrong-token")
		rec := httptest.NewRecorder()
		csrf.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST with bad token returned %d, want 403", rec.Code)
		}
	})

	t.Run("git endpoints bypass CSRF", func(t *testing.T) {
		for _, path := range []string{"/owner/repo/info/refs", "/owner/repo/git-upload-pack", "/owner/repo/git-receive-pack"} {
			req := httptest.NewRequest("POST", path, nil)
			rec := httptest.NewRecorder()
			csrf.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("git endpoint %s returned %d, want 200", path, rec.Code)
			}
		}
	})

	t.Run("Bearer token bypasses CSRF", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/foo", nil)
		req.Header.Set("Authorization", "Bearer valid-token-here")
		rec := httptest.NewRecorder()
		csrf.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Bearer request returned %d, want 200", rec.Code)
		}
	})

	t.Run("garbage Authorization header does NOT bypass CSRF", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/foo", nil)
		req.Header.Set("Authorization", "garbage")
		rec := httptest.NewRecorder()
		csrf.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("garbage auth header returned %d, want 403", rec.Code)
		}
	})

	t.Run("empty Bearer does NOT bypass CSRF", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/foo", nil)
		req.Header.Set("Authorization", "Bearer ")
		rec := httptest.NewRecorder()
		csrf.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("empty Bearer returned %d, want 403", rec.Code)
		}
	})

	t.Run("Secure flag set when configured", func(t *testing.T) {
		secureCsrf := CSRF(true)(okHandler)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		secureCsrf.ServeHTTP(rec, req)
		for _, c := range rec.Result().Cookies() {
			if c.Name == "csrf_token" && !c.Secure {
				t.Error("csrf_token cookie should have Secure flag when configured")
			}
		}
	})
}

package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// CSRF implements double-submit cookie CSRF protection.
// It sets a non-HttpOnly cookie with a random token and validates that
// state-changing requests include the same token in the X-CSRF-Token header
// (for HTMX/AJAX) or in a "csrf_token" form field (for plain HTML forms).
func CSRF(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read or generate CSRF token
			cookie, err := r.Cookie("csrf_token")
			var token string
			if err != nil || cookie.Value == "" {
				token = generateCSRFToken()
				http.SetCookie(w, &http.Cookie{
					Name:     "csrf_token",
					Value:    token,
					Path:     "/",
					HttpOnly: false, // JS/HTMX must read this
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
				})
			} else {
				token = cookie.Value
			}

			// Safe methods don't need validation
			if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
				next.ServeHTTP(w, r)
				return
			}

			// Skip CSRF for git smart protocol endpoints (they use their own auth)
			if isGitEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Skip CSRF for API endpoints using Bearer token (not cookie-based).
			// Require a valid "Bearer <token>" format to prevent bypass with garbage headers.
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") && len(auth) > 7 {
				next.ServeHTTP(w, r)
				return
			}

			// Validate CSRF token from header (HTMX/AJAX) or form field (plain HTML forms)
			submitted := r.Header.Get("X-CSRF-Token")
			if submitted == "" {
				submitted = r.FormValue("csrf_token")
			}
			if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("csrf: failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func isGitEndpoint(path string) bool {
	return len(path) > 0 &&
		// Git smart protocol endpoints use their own auth via Basic Auth or PAT
		(strings.HasSuffix(path, "/info/refs") ||
			strings.HasSuffix(path, "/git-upload-pack") ||
			strings.HasSuffix(path, "/git-receive-pack"))
}

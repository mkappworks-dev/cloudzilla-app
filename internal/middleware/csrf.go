package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// CSRF implements double-submit cookie CSRF protection.
// It sets a non-HttpOnly cookie with a random token and validates that
// state-changing requests include the same token in the X-CSRF-Token header.
// HTMX sends this automatically via hx-headers on the body element.
func CSRF(next http.Handler) http.Handler {
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
		path := r.URL.Path
		if isGitEndpoint(path) {
			next.ServeHTTP(w, r)
			return
		}

		// Skip CSRF for API endpoints using Bearer token (not cookie-based)
		if auth := r.Header.Get("Authorization"); auth != "" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate CSRF token from header
		headerToken := r.Header.Get("X-CSRF-Token")
		if headerToken == "" || headerToken != token {
			http.Error(w, "CSRF token mismatch", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func isGitEndpoint(path string) bool {
	return len(path) > 0 &&
		// Git smart protocol endpoints use their own auth via Basic Auth or PAT
		(endsWith(path, "/info/refs") ||
			endsWith(path, "/git-upload-pack") ||
			endsWith(path, "/git-receive-pack"))
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

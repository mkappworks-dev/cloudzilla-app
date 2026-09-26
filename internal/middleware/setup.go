package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// RequireSetup redirects every request to /setup until the instance is configured, except the setup page, invite links, and static assets.
func RequireSetup(svc *service.SiteSettingService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			if path == "/setup" ||
				strings.HasPrefix(path, "/static/") ||
				strings.HasPrefix(path, "/invite/") ||
				path == "/htmx.min.js" ||
				path == "/alpine.min.js" {
				next.ServeHTTP(w, r)
				return
			}

			if !svc.IsSetupComplete(context.Background()) {
				http.Redirect(w, r, "/setup", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

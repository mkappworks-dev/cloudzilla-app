package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// RequireSetup redirects all requests to /setup when setup is not complete.
// Allows /setup, /static/, and /invite/ through unconditionally.
// RequireSetup redirects all requests to /setup until the instance has been configured.
func RequireSetup(svc *service.SiteSettingService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// Always allow these paths through
			if path == "/setup" ||
				strings.HasPrefix(path, "/static/") ||
				strings.HasPrefix(path, "/invite/") ||
				path == "/htmx.min.js" {
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

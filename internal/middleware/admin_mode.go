package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/adminmode"
)

type AdminModes interface {
	Resolve(ctx context.Context, r *http.Request, raw string, userID int64, username string, sessionVersion int) (time.Time, bool)
	RecordAction(ctx context.Context, r *http.Request, userID int64, username, targetType, targetName string, status int)
}

// WithAdminMode honours the admin-mode cookie for browser sessions only; routes that must not (git smart HTTP)
// use an auth middleware built without it.
func WithAdminMode(cookieName string, v AdminModes) AuthOption {
	return func(o *authOptions) {
		o.adminModes = v
		o.adminCookie = cookieName
	}
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// withAdminMode serves next with admin mode bound to claims' user when r is a
// browser session and its admin-mode cookie is running. A mutating request that
// the override let through is audited after it finishes.
func (o authOptions) withAdminMode(w http.ResponseWriter, r *http.Request, claims Claims, sessionCookie string, next http.Handler) {
	if o.adminModes == nil || !claims.IsSuperadmin || claims.Scoped || !CameByCookie(r, sessionCookie) {
		next.ServeHTTP(w, r)
		return
	}
	ck, err := r.Cookie(o.adminCookie)
	if err != nil {
		next.ServeHTTP(w, r)
		return
	}
	expiresAt, ok := o.adminModes.Resolve(r.Context(), r, ck.Value, claims.UserID, claims.Username, claims.SessionVersion)
	if !ok {
		next.ServeHTTP(w, r)
		return
	}
	ctx, st := adminmode.With(r.Context(), claims.UserID, expiresAt)
	ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
	next.ServeHTTP(ww, r.WithContext(ctx))
	if st.Used() && isMutating(r.Method) {
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK // net/http's implicit status when a handler only writes a body
		}
		targetType, target := adminTarget(r)
		o.adminModes.RecordAction(context.WithoutCancel(ctx), r, claims.UserID, claims.Username, targetType, target, status)
	}
}

// CameByCookie reports whether the session token was read from the cookie, not
// a bearer header, which extractToken prefers.
func CameByCookie(r *http.Request, cookieName string) bool {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return false
	}
	_, err := r.Cookie(cookieName)
	return err == nil
}

// adminTarget names the repository or organization a request's route addresses.
func adminTarget(r *http.Request) (string, string) {
	owner, repo, org := chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), chi.URLParam(r, "org")
	switch {
	case owner != "" && repo != "":
		return "repo", owner + "/" + repo
	case org != "":
		return "org", org
	}
	return "", ""
}

package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const adminModeFormError = "#admin-mode-error"

// Derived from the session cookie's name so servers sharing a host keep distinct ones.
func AdminModeCookie(sessionCookie string) string { return sessionCookie + "_admin" }

func (h *Handler) adminModeCookieName() string { return AdminModeCookie(h.Cfg.Auth.CookieName) }

// setAdminModeCookie sets the cookie until expiresAt; a zero expiresAt clears it.
func (h *Handler) setAdminModeCookie(w http.ResponseWriter, value string, expiresAt time.Time) {
	c := &http.Cookie{
		Name: h.adminModeCookieName(), Value: value, Path: "/",
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	}
	if expiresAt.IsZero() {
		c.MaxAge = -1
	} else {
		c.Expires = expiresAt
	}
	http.SetCookie(w, c)
}

func (h *Handler) PageAdminMode(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		h.Forbidden(w, r)
		return
	}
	h.render(w, r, pages.AdminMode(view.AdminModeData{
		BasePage: basePage(r, h.Services),
		Confirm:  h.confirmFactors(r, claims.UserID),
		Next:     safeNextPath(r.FormValue("next")),
	}))
}

// EnterAdminMode handles POST /admin/mode; tokens and bearer sessions are refused because they never honour the cookie.
func (h *Handler) EnterAdminMode(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin || claims.Scoped || !middleware.CameByCookie(r, h.Cfg.Auth.CookieName) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), adminModeFormError) {
		return
	}
	raw, expiresAt, err := h.Services.AdminMode.Enter(r.Context(), r, claims.UserID, claims.Username, claims.SessionVersion)
	if err != nil {
		slog.Error("enter admin mode", "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "couldn't enter admin mode")
		return
	}
	h.setAdminModeCookie(w, raw, expiresAt)
	redirectAfterAdminMode(w, r)
}

// LeaveAdminMode works for any signed-in session so a demoted user can still drop the cookie.
func (h *Handler) LeaveAdminMode(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	h.endAdminMode(w, r, claims)
	redirectAfterAdminMode(w, r)
}

func redirectAfterAdminMode(w http.ResponseWriter, r *http.Request) {
	next := safeNextPath(r.FormValue("next"))
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", next)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// forbiddenSettings answers a settings page the viewer may not manage. A
// superadmin outside admin mode is pointed at it; anyone else gets the plain 403.
func (h *Handler) forbiddenSettings(w http.ResponseWriter, r *http.Request, isSuperadmin bool) {
	if !isSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusForbidden)
	h.render(w, r, pages.ForbiddenAdminMode(basePage(r, h.Services), r.URL.RequestURI()))
}

// endAdminMode ends this browser's admin mode, if any, and clears its cookie.
func (h *Handler) endAdminMode(w http.ResponseWriter, r *http.Request, claims middleware.Claims) {
	if ck, err := r.Cookie(h.adminModeCookieName()); err == nil {
		h.Services.AdminMode.Leave(r.Context(), r, ck.Value, claims.UserID, claims.Username)
	}
	h.setAdminModeCookie(w, "", time.Time{})
}

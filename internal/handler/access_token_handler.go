package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

func (h *Handler) CreateToken(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		redirectReauthRefusal(w, r, claims.UserID, err, "tokens")
		return
	}

	scopes := r.Form["scopes"]

	var expiresAt *time.Time
	if exp := r.FormValue("expires_at"); exp != "" {
		t, err := time.Parse("2006-01-02", exp)
		if err == nil {
			expiresAt = &t
		}
	}

	rawToken, _, err := h.Services.AccessToken.Generate(r.Context(), claims.UserID, name, scopes, expiresAt)
	switch {
	case errors.Is(err, service.ErrAdminTokenNoExpiry):
		http.Redirect(w, r, "/settings?profile_error=token_admin_expiry#tokens", http.StatusSeeOther)
		return
	case errors.Is(err, service.ErrUnknownTokenScope):
		http.Redirect(w, r, "/settings?profile_error=token_scope_unknown#tokens", http.StatusSeeOther)
		return
	case err != nil:
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		return
	}

	h.setSettingsFlash(w, newTokenCookieName, rawToken)
	http.Redirect(w, r, "/settings#tokens", http.StatusSeeOther)
}

func (h *Handler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.Services.AccessToken.Delete(r.Context(), id, claims.UserID); err != nil {
		http.Error(w, "failed to delete token", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		tokens, _ := h.Services.AccessToken.List(r.Context(), claims.UserID)
		h.render(w, r, fragments.TokensList(view.TokensListFragData{Tokens: tokens}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

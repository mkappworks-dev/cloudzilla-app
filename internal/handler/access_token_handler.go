package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
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
		settingsError(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := r.ParseForm(); err != nil {
		settingsError(w, r, http.StatusBadRequest, "bad request")
		return
	}
	htmx := r.Header.Get("HX-Request") == "true"
	refuse := func(code string) { refuseSettingsForm(w, r, tokenFormError, "tokens", code) }

	name := r.FormValue("name")
	if name == "" {
		if htmx {
			renderFormError(w, tokenFormError, "name is required")
			return
		}
		http.Error(w, "name is required", http.StatusBadRequest)
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

	nt, err := h.Services.AccessToken.Check(r.Context(), claims.UserID, service.NewToken{
		Name: name, Scopes: scopes, ExpiresAt: expiresAt,
		SigningKey: r.FormValue("signing_key"),
		Targets:    strings.FieldsFunc(r.FormValue("targets"), func(c rune) bool { return c == ',' || c == '\n' || c == '\r' }),
	})
	switch {
	case errors.Is(err, service.ErrScopeRequired) || errors.Is(err, service.ErrInvalidScope):
		if htmx {
			renderFormError(w, tokenFormError, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, service.ErrAdminTokenNeedsKey):
		refuse("token_admin_key")
		return
	case errors.Is(err, service.ErrInvalidSigningKey):
		refuse("token_key_invalid")
		return
	case errors.Is(err, service.ErrAdminTokenNoExpiry):
		refuse("token_admin_expiry")
		return
	case errors.Is(err, service.ErrAdminTokenNeedsTargets):
		refuse("token_admin_targets")
		return
	case errors.Is(err, service.ErrTokenTarget):
		refuse("token_target")
		return
	case err != nil:
		settingsError(w, r, http.StatusInternalServerError, "failed to create token")
		return
	}
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		redirectReauthRefusal(w, r, claims.UserID, err, tokenFormError, "tokens")
		return
	}

	rawToken, _, err := h.Services.AccessToken.Create(r.Context(), claims.UserID, nt)
	if err != nil {
		settingsError(w, r, http.StatusInternalServerError, "failed to create token")
		return
	}

	// The flash cookie rides on the HX-Redirect response, so the reload shows the token.
	h.setSettingsFlash(w, newTokenCookieName, rawToken)
	redirectAfterSave(w, r, "/settings#tokens")
}

const tokenFormError = "#generate-token-form-error"

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

package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageTokens(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tokens, err := h.Services.AccessToken.List(r.Context(), claims.UserID)
	if err != nil || tokens == nil {
		tokens = nil
	}

	data := view.TokensData{
		BasePage: basePage(r, h.Services),
		Tokens:   tokens,
		NewToken: r.URL.Query().Get("new_token"),
	}
	h.render(w, r, pages.Tokens(data))
}

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

	scopes := r.Form["scopes"]

	var expiresAt *time.Time
	if exp := r.FormValue("expires_at"); exp != "" {
		t, err := time.Parse("2006-01-02", exp)
		if err == nil {
			expiresAt = &t
		}
	}

	rawToken, _, err := h.Services.AccessToken.Generate(r.Context(), claims.UserID, name, scopes, expiresAt)
	if err != nil {
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings/tokens?new_token="+rawToken, http.StatusSeeOther)
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

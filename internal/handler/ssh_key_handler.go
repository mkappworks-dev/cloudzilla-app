package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

func (h *Handler) AddSSHKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Title     string `json:"title"`
		PublicKey string `json:"public_key"`
	}

	// Handle form data from HTMX or JSON
	if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		req.Title = r.FormValue("title")
		req.PublicKey = r.FormValue("public_key")
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	if req.Title == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "title and public_key are required")
		return
	}

	key, err := h.Services.SSHKey.AddKey(r.Context(), claims.UserID, req.Title, req.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check if this is an HTMX request - if so, return the updated list
	if r.Header.Get("HX-Request") == "true" {
		keys, _ := h.Services.SSHKey.ListByUser(r.Context(), claims.UserID)
		if keys == nil {
			keys = []model.SSHKey{}
		}
		h.render(w, r, fragments.SSHKeysList(view.SSHKeysFragData{SSHKeys: keys}))
		return
	}

	// Otherwise return JSON
	writeJSON(w, http.StatusCreated, key)
}

func (h *Handler) ListSSHKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keys, err := h.Services.SSHKey.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}

	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) DeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keyID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	if err := h.Services.SSHKey.Delete(r.Context(), keyID, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete key")
		return
	}

	// Check if this is an HTMX request - if so, return the updated list
	if r.Header.Get("HX-Request") == "true" {
		keys, _ := h.Services.SSHKey.ListByUser(r.Context(), claims.UserID)
		if keys == nil {
			keys = []model.SSHKey{}
		}
		h.render(w, r, fragments.SSHKeysList(view.SSHKeysFragData{SSHKeys: keys}))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

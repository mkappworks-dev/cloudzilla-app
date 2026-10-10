package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// CreateGist handles POST /api/gists.
func (h *Handler) CreateGist(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Description string           `json:"description"`
		Public      bool             `json:"public"`
		Files       []model.GistFile `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	g, err := h.Services.Gist.Create(r.Context(), claims.UserID, claims.Username, body.Description, body.Public, body.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

// UpdateGist handles PATCH /api/gists/{id}.
func (h *Handler) UpdateGist(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	var body struct {
		Description string           `json:"description"`
		Public      bool             `json:"public"`
		Files       []model.GistFile `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := h.Services.Gist.Update(r.Context(), id, claims.UserID, body.Description, body.Public, body.Files); err != nil {
		switch {
		case errors.Is(err, service.ErrGistNotFound):
			writeError(w, http.StatusNotFound, "gist not found")
		case errors.Is(err, service.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden")
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteGist handles DELETE /api/gists/{id}.
func (h *Handler) DeleteGist(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	if err := h.Services.Gist.Delete(r.Context(), id, claims.UserID); err != nil {
		switch {
		case errors.Is(err, service.ErrGistNotFound):
			writeError(w, http.StatusNotFound, "gist not found")
		case errors.Is(err, service.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden")
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddFileFragment returns an HTMX fragment for a new gist file row.
func (h *Handler) AddFileFragment(w http.ResponseWriter, r *http.Request) {
	idx, _ := strconv.Atoi(r.URL.Query().Get("index"))
	h.render(w, r, fragments.GistFileRow(idx))
}

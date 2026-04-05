package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// PageGists renders the public gist explore page.
func (h *Handler) PageGists(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	gists, _ := h.Services.Gist.Explore(r.Context(), page, 20)
	if gists == nil {
		gists = []model.Gist{}
	}
	h.render(w, r, pages.Gists(view.GistsData{
		BasePage: basePage(r, h.Services),
		Gists:    gists,
		Page:     page,
	}))
}

// PageGistNew renders the new gist form.
func (h *Handler) PageGistNew(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.GistNew(view.GistNewData{BasePage: basePage(r, h.Services)}))
}

// PageGistDetail renders a gist's detail page.
func (h *Handler) PageGistDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, files, err := h.Services.Gist.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "gist not found", http.StatusNotFound)
		return
	}
	if files == nil {
		files = []model.GistFile{}
	}
	isOwner := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		isOwner = claims.UserID == g.OwnerID
	}
	// Private gists are only visible to their owner.
	if !g.Public && !isOwner {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.render(w, r, pages.GistDetail(view.GistDetailData{
		BasePage: basePage(r, h.Services),
		Gist:     *g,
		Files:    files,
		IsOwner:  isOwner,
	}))
}

// PageGistEdit renders the gist edit form.
func (h *Handler) PageGistEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	g, files, err := h.Services.Gist.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "gist not found", http.StatusNotFound)
		return
	}
	if g.OwnerID != claims.UserID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if files == nil {
		files = []model.GistFile{}
	}
	h.render(w, r, pages.GistEdit(view.GistEditData{
		BasePage: basePage(r, h.Services),
		Gist:     *g,
		Files:    files,
	}))
}

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
		writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PageUserGists renders a user's gist listing.
func (h *Handler) PageUserGists(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "owner")
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	user, err := h.Services.User.GetByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Determine whether the viewer is the owner (can see private gists).
	isOwner := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		isOwner = claims.UserID == user.ID
	}

	var gists []model.Gist
	if isOwner {
		gists, _ = h.Services.Gist.ListByOwner(r.Context(), user.ID, page, 20)
	} else {
		// Non-owners only see public gists; filter after listing.
		all, _ := h.Services.Gist.ListByOwner(r.Context(), user.ID, page, 20)
		for _, g := range all {
			if g.Public {
				gists = append(gists, g)
			}
		}
	}
	if gists == nil {
		gists = []model.Gist{}
	}

	h.render(w, r, pages.UserGists(view.UserGistsData{
		BasePage:    basePage(r, h.Services),
		ProfileUser: *user,
		Gists:       gists,
		Page:        page,
	}))
}

// AddFileFragment returns an HTMX fragment for a new gist file row.
func (h *Handler) AddFileFragment(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, fragments.GistFileRow())
}

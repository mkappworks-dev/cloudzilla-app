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
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageGists renders the public gist explore page, with a Secret tab for the signed-in viewer.
func (h *Handler) PageGists(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	tab := r.URL.Query().Get("tab")
	if tab != "secret" {
		tab = "public"
	}

	ctx := r.Context()
	claims, signedIn := middleware.ClaimsFromContext(ctx)

	if tab == "secret" && !signedIn {
		tab = "public"
	}

	var rows []model.GistListRow
	if tab == "secret" && signedIn {
		privateGists, _ := h.Services.Gist.ListPrivateByOwner(ctx, claims.UserID, page, 50)
		for _, g := range privateGists {
			rows = append(rows, model.GistListRow{Gist: g})
		}
	} else {
		rows, _ = h.Services.Gist.ListWithCounts(ctx, "")
	}
	if rows == nil {
		rows = []model.GistListRow{}
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	filenamesByGist, _ := h.Services.Gist.LoadFilenames(ctx, ids)

	items := make([]view.GistListItem, 0, len(rows))
	for _, r := range rows {
		files := filenamesByGist[r.ID]
		label, chipClass := gistLanguage(files)
		r.FileCount = int64(len(files))
		items = append(items, view.GistListItem{GistListRow: r, LanguageLabel: label, LanguageClass: chipClass})
	}

	data := view.GistsData{
		BasePage:           basePage(r, h.Services),
		Gists:              items,
		Page:               page,
		Tab:                tab,
		SecretTabAvailable: signedIn,
	}
	if signedIn {
		data.BasePage = withAccountSubnav(data.BasePage, "gists", h.accountCounts(ctx, claims.UserID))
	}
	h.render(w, r, pages.Gists(data))
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
		gists, _ = h.Services.Gist.ListPublicByOwner(r.Context(), user.ID, page, 20)
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

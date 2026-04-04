package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// PageWikiHome redirects /{owner}/{repo}/wiki → /{owner}/{repo}/wiki/Home.
func (h *Handler) PageWikiHome(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/wiki/Home", http.StatusFound)
}

// PageWikiPage renders a single wiki page (read view).
func (h *Handler) PageWikiPage(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	slug := chi.URLParam(r, "slug")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	pageList, _ := h.Services.Code.WikiPageList(owner, repoName)
	if pageList == nil {
		pageList = []string{}
	}

	rawContent, err := h.Services.Code.WikiPageGet(owner, repoName, slug)
	if err != nil {
		http.Error(w, "failed to load wiki page", http.StatusInternalServerError)
		return
	}

	exists := rawContent != ""

	h.render(w, r, pages.WikiPage(view.WikiPageData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Slug:        slug,
		ContentHTML: markdown.Render(rawContent),
		PageList:    pageList,
		CanWrite:    canWrite,
		Exists:      exists,
	}))
}

// PageWikiEdit renders the wiki editor for a page (create or edit).
func (h *Handler) PageWikiEdit(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	slug := chi.URLParam(r, "slug")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rawContent, err := h.Services.Code.WikiPageGet(owner, repoName, slug)
	if err != nil {
		rawContent = ""
	}

	h.render(w, r, pages.WikiEdit(view.WikiEditData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Slug:     slug,
		Content:  rawContent,
		CanWrite: true,
	}))
}

// CreateOrUpdateWikiPage handles POST /api/repos/{owner}/{repo}/wiki/{slug}.
// Accepts form values: content, message (optional commit message).
func (h *Handler) CreateOrUpdateWikiPage(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	slug := chi.URLParam(r, "slug")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form data")
		return
	}
	content := r.FormValue("content")
	message := r.FormValue("message")

	user, err := h.Services.User.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	authorEmail := user.Email
	if authorEmail == "" {
		authorEmail = user.Username + "@localhost"
	}

	if err := h.Services.Code.WikiPageSave(owner, repoName, slug, content, user.Username, authorEmail, message); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	http.Redirect(w, r, "/"+owner+"/"+repoName+"/wiki/"+slug, http.StatusSeeOther)
}

// DeleteWikiPage handles DELETE /api/repos/{owner}/{repo}/wiki/{slug}.
func (h *Handler) DeleteWikiPage(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	slug := chi.URLParam(r, "slug")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	user, err := h.Services.User.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	authorEmail := user.Email
	if authorEmail == "" {
		authorEmail = user.Username + "@localhost"
	}

	if err := h.Services.Code.WikiPageDelete(owner, repoName, slug, user.Username, authorEmail); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	http.Redirect(w, r, "/"+owner+"/"+repoName+"/wiki", http.StatusSeeOther)
}

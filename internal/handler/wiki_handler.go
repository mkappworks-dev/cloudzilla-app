package handler

import (
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// validWikiSlug restricts page names to safe alphanumeric slugs to prevent
// path traversal into the bare wiki git repo.
var validWikiSlug = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

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

	if !validWikiSlug.MatchString(slug) {
		http.Error(w, "invalid page name", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !repo.AllowWiki {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Gate private repos: check read permission before serving any wiki content.
	var uid *int64
	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		uid = &claims.UserID
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, uid) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	pageList, err := h.Services.Code.WikiPageList(owner, repoName)
	if err != nil {
		slog.Error("failed to list wiki pages", "owner", owner, "repo", repoName, "error", err)
	}
	if pageList == nil {
		pageList = []string{}
	}

	rawContent, exists, err := h.Services.Code.WikiPageGet(owner, repoName, slug)
	if err != nil {
		http.Error(w, "failed to load wiki page", http.StatusInternalServerError)
		return
	}

	h.render(w, r, pages.WikiPage(view.WikiPageData{
		BasePage:    withRepoSubnav(basePage(r, h.Services), repo, "wiki", canManage),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Slug:        slug,
		ContentHTML: markdown.Render(rawContent),
		PageList:    pageList,
		CanWrite:    canWrite,
		CanManage:   canManage,
		Exists:      exists,
	}))
}

// PageWikiEdit renders the wiki editor for a page (create or edit).
func (h *Handler) PageWikiEdit(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	slug := chi.URLParam(r, "slug")

	if !validWikiSlug.MatchString(slug) {
		http.Error(w, "invalid page name", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !repo.AllowWiki {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Gate private repos: check read permission before serving any wiki content.
	if !h.Services.Repo.CanRead(r.Context(), repo, &claims.UserID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rawContent, _, err := h.Services.Code.WikiPageGet(owner, repoName, slug)
	if err != nil {
		slog.Error("failed to load wiki page for editing", "owner", owner, "repo", repoName, "slug", slug, "error", err)
		http.Error(w, "failed to load page content", http.StatusInternalServerError)
		return
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.WikiEdit(view.WikiEditData{
		BasePage: withRepoSubnav(basePage(r, h.Services), repo, "wiki", canManage),
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

	if !validWikiSlug.MatchString(slug) {
		writeError(w, http.StatusBadRequest, "invalid page name")
		return
	}

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
	if !repo.AllowWiki {
		writeError(w, http.StatusNotFound, "not found")
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
		slog.Error("failed to save wiki page", "owner", owner, "repo", repoName, "slug", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save wiki page")
		return
	}

	http.Redirect(w, r, "/"+owner+"/"+repoName+"/wiki/"+slug, http.StatusSeeOther)
}

// DeleteWikiPage handles DELETE /api/repos/{owner}/{repo}/wiki/{slug}.
func (h *Handler) DeleteWikiPage(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	slug := chi.URLParam(r, "slug")

	if !validWikiSlug.MatchString(slug) {
		writeError(w, http.StatusBadRequest, "invalid page name")
		return
	}

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
	if !repo.AllowWiki {
		writeError(w, http.StatusNotFound, "not found")
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
		slog.Error("failed to delete wiki page", "owner", owner, "repo", repoName, "slug", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete wiki page")
		return
	}

	http.Redirect(w, r, "/"+owner+"/"+repoName+"/wiki", http.StatusSeeOther)
}

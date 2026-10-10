package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
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

	if !validWikiSlug.MatchString(slug) {
		http.Error(w, "invalid page name", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !h.Services.Repo.WikiEnabled(r.Context(), repo) {
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

	pageList, err := h.Services.Code.WikiPageListMeta(owner, repoName)
	if err != nil {
		slog.Error("failed to list wiki pages", "owner", owner, "repo", repoName, "error", err)
		pageList = []service.WikiPageMeta{}
	}

	rawContent, exists, err := h.Services.Code.WikiPageGet(owner, repoName, slug)
	if err != nil {
		http.Error(w, "failed to load wiki page", http.StatusInternalServerError)
		return
	}

	h.render(w, r, pages.WikiPage(view.WikiPageData{
		BasePage:    h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "wiki", canManage),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Slug:        slug,
		ContentHTML: markdown.RenderCtx(r.Context(), rawContent),
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
	if !h.Services.Repo.WikiEnabled(r.Context(), repo) {
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

	pageList, err := h.Services.Code.WikiPageListMeta(owner, repoName)
	if err != nil {
		slog.Error("failed to list wiki pages", "owner", owner, "repo", repoName, "error", err)
		pageList = []service.WikiPageMeta{}
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.WikiEdit(view.WikiEditData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "wiki", canManage),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Slug:     slug,
		Content:  rawContent,
		PageList: pageList,
		CanWrite: true,
	}))
}

func (h *Handler) PageWikiNew(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !h.Services.Repo.WikiEnabled(r.Context(), repo) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, &claims.UserID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	pageList, err := h.Services.Code.WikiPageListMeta(owner, repoName)
	if err != nil {
		slog.Error("failed to list wiki pages", "owner", owner, "repo", repoName, "error", err)
		pageList = []service.WikiPageMeta{}
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.WikiNew(view.WikiNewData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "wiki", canManage),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		PageList: pageList,
	}))
}

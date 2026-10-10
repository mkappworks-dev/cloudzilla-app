package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// validWikiSlug restricts page names to safe alphanumeric slugs to prevent
// path traversal into the bare wiki git repo.
var validWikiSlug = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

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

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	if !h.Services.Repo.WikiEnabled(r.Context(), repo) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if status, msg := h.storageRefusal(r, repo); status != 0 {
		writeError(w, status, msg)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form data")
		return
	}
	content := r.FormValue("content")
	message := r.FormValue("message")
	newSlug := r.FormValue("new_slug")

	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	if newSlug != "" && newSlug != slug {
		if !validWikiSlug.MatchString(newSlug) {
			writeError(w, http.StatusBadRequest, "invalid new page name")
			return
		}
		_, exists, err := h.Services.Code.WikiPageGet(owner, repoName, newSlug)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check page existence")
			return
		}
		if exists {
			writeError(w, http.StatusConflict, "a page with that name already exists")
			return
		}
		renameMsg := "Rename " + slug + " to " + newSlug
		if err := h.Services.Code.WikiPageRename(owner, repoName, slug, newSlug, author, renameMsg); err != nil {
			if errors.Is(err, service.ErrWikiPageExists) {
				writeError(w, http.StatusConflict, "a page with that name already exists")
				return
			}
			if errors.Is(err, service.ErrWikiPageNotFound) {
				writeError(w, http.StatusNotFound, "wiki page not found")
				return
			}
			if errors.Is(err, service.ErrRefMoved) {
				writeError(w, http.StatusConflict, branchMovedMsg)
				return
			}
			slog.Error("failed to rename wiki page", "owner", owner, "repo", repoName, "slug", slug, "newSlug", newSlug, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to rename wiki page")
			return
		}
		slug = newSlug
	}

	if err := h.Services.Code.WikiPageSave(owner, repoName, slug, content, author, message); err != nil {
		if errors.Is(err, service.ErrRefMoved) {
			writeError(w, http.StatusConflict, branchMovedMsg)
			return
		}
		slog.Error("failed to save wiki page", "owner", owner, "repo", repoName, "slug", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save wiki page")
		return
	}
	h.Services.Quota.Recompute(repo)

	http.Redirect(w, r, "/"+owner+"/"+repoName+"/wiki/"+slug, http.StatusSeeOther)
}

func (h *Handler) WikiSetPageOrder(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	if !h.Services.Repo.WikiEnabled(r.Context(), repo) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if status, msg := h.storageRefusal(r, repo); status != 0 {
		writeError(w, status, msg)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form data")
		return
	}

	raw := r.FormValue("order")
	parts := strings.Split(raw, ",")
	slugs := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		if !validWikiSlug.MatchString(s) {
			writeError(w, http.StatusBadRequest, "invalid page name: "+s)
			return
		}
		slugs = append(slugs, s)
	}

	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	if err := h.Services.Code.WikiPageSetOrder(owner, repoName, slugs, author); err != nil {
		if errors.Is(err, service.ErrRefMoved) {
			writeError(w, http.StatusConflict, branchMovedMsg)
			return
		}
		slog.Error("failed to set wiki page order", "owner", owner, "repo", repoName, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set wiki page order")
		return
	}
	h.Services.Quota.Recompute(repo)

	w.WriteHeader(http.StatusOK)
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

	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	if !h.Services.Repo.WikiEnabled(r.Context(), repo) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	if err := h.Services.Code.WikiPageDelete(owner, repoName, slug, author); err != nil {
		if errors.Is(err, service.ErrRefMoved) {
			writeError(w, http.StatusConflict, branchMovedMsg)
			return
		}
		slog.Error("failed to delete wiki page", "owner", owner, "repo", repoName, "slug", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete wiki page")
		return
	}
	h.Services.Quota.Recompute(repo)

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/wiki", http.StatusSeeOther)
}

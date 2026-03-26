package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

func (h *Handler) ListBranchProtections(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	rules, err := h.Services.BranchProtection.List(r.Context(), repo.ID)
	if err != nil {
		http.Error(w, "failed to list branch protections", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (h *Handler) CreateBranchProtection(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	pattern := strings.TrimSpace(r.FormValue("pattern"))
	if pattern == "" {
		http.Error(w, "pattern is required", http.StatusBadRequest)
		return
	}

	requireReviewCount, _ := strconv.Atoi(r.FormValue("require_review_count"))
	blockForcePush := r.FormValue("block_force_push") == "true"

	var statusChecks model.StringSlice
	if raw := strings.TrimSpace(r.FormValue("require_status_checks")); raw != "" {
		for _, c := range strings.Split(raw, ",") {
			if c = strings.TrimSpace(c); c != "" {
				statusChecks = append(statusChecks, c)
			}
		}
	}

	bp := &model.BranchProtection{
		RepoID:              repo.ID,
		Pattern:             pattern,
		RequireReviewCount:  requireReviewCount,
		RequireStatusChecks: statusChecks,
		BlockForcePush:      blockForcePush,
	}
	if err := h.Services.BranchProtection.Create(r.Context(), bp); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		rules, _ := h.Services.BranchProtection.List(r.Context(), repo.ID)
		h.render(w, r, fragments.BranchProtections(view.BranchProtectionsFragData{
			Owner:     owner,
			RepoName:  repoName,
			RepoID:    repo.ID,
			Rules:     rules,
			CanManage: true,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, bp)
}

func (h *Handler) UpdateBranchProtection(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	requireReviewCount, _ := strconv.Atoi(r.FormValue("require_review_count"))
	blockForcePush := r.FormValue("block_force_push") == "true"

	var statusChecks model.StringSlice
	if raw := strings.TrimSpace(r.FormValue("require_status_checks")); raw != "" {
		for _, c := range strings.Split(raw, ",") {
			if c = strings.TrimSpace(c); c != "" {
				statusChecks = append(statusChecks, c)
			}
		}
	}

	if err := h.Services.BranchProtection.Update(r.Context(), id, repo.ID, requireReviewCount, statusChecks, blockForcePush); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		rules, _ := h.Services.BranchProtection.List(r.Context(), repo.ID)
		h.render(w, r, fragments.BranchProtections(view.BranchProtectionsFragData{
			Owner:     owner,
			RepoName:  repoName,
			RepoID:    repo.ID,
			Rules:     rules,
			CanManage: true,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteBranchProtection(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.Services.BranchProtection.Delete(r.Context(), id, repo.ID); err != nil {
		http.Error(w, "failed to delete branch protection", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		rules, _ := h.Services.BranchProtection.List(r.Context(), repo.ID)
		h.render(w, r, fragments.BranchProtections(view.BranchProtectionsFragData{
			Owner:     owner,
			RepoName:  repoName,
			RepoID:    repo.ID,
			Rules:     rules,
			CanManage: true,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

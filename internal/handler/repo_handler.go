package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

type createRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

func (h *Handler) ListRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := h.Services.Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos")
		return
	}
	if repos == nil {
		repos = make([]model.Repository, 0)
	}
	writeJSON(w, http.StatusOK, repos)
}

func (h *Handler) GetRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	name := chi.URLParam(r, "repo")
	repo, err := h.Services.Repo.Get(r.Context(), owner, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (h *Handler) CreateRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	repo, err := h.Services.Repo.Create(r.Context(), claims.Username, req.Name, req.Description, req.Private)
	if err != nil {
		slog.Error("failed to create repo", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create repository")
		return
	}

	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoCreate, "repo", repo.ID, repo.Name, nil)

	writeJSON(w, http.StatusCreated, repo)
}

func (h *Handler) ListUserRepos(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "username")
	repos, err := h.Services.Repo.ListByOwner(r.Context(), owner)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if repos == nil {
		repos = make([]model.Repository, 0)
	}
	writeJSON(w, http.StatusOK, repos)
}

func (h *Handler) ListCollaborators(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	collabs, err := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list collaborators")
		return
	}
	writeJSON(w, http.StatusOK, collabs)
}

func (h *Handler) AddCollaborator(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

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

	username := r.FormValue("username")
	role := r.FormValue("role")
	if username == "" || role == "" {
		writeError(w, http.StatusBadRequest, "username and role are required")
		return
	}

	if err := h.Services.Repo.AddCollaborator(r.Context(), repo.ID, username, role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	repoID := repo.ID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventMemberAdded, map[string]any{"username": username})

	if r.Header.Get("HX-Request") == "true" {
		collabs, _ := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
		if collabs == nil {
			collabs = []model.Permission{}
		}
		h.render(w, r, fragments.RepoCollaborators(view.RepoCollaboratorsFragData{
			Owner:    owner,
			RepoName: repoName,
			RepoID:   repo.ID,
			Collabs:  collabs,
			CanManage: true,
		}))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handler) TransferRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

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

	if !h.Services.Repo.IsOwner(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	newOwner := r.FormValue("new_owner")
	if newOwner == "" {
		writeError(w, http.StatusBadRequest, "new_owner is required")
		return
	}

	if err := h.Services.Repo.TransferRepo(r.Context(), repo, claims.UserID, newOwner); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "transfer failed")
		return
	}

	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoTransfer, "repo", repo.ID, repo.Name, nil)

	http.Redirect(w, r, "/"+newOwner+"/"+repoName, http.StatusSeeOther)
}

func (h *Handler) RemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

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

	var userID int64
	if v := r.URL.Query().Get("user_id"); v != "" {
		var parseErr error
		userID, parseErr = strconv.ParseInt(v, 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid user_id")
			return
		}
	} else {
		var body struct {
			UserID int64 `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			userID = body.UserID
		}
	}

	if userID == 0 {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	if err := h.Services.Repo.RemoveCollaborator(r.Context(), repo.ID, userID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		collabs, _ := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
		if collabs == nil {
			collabs = []model.Permission{}
		}
		h.render(w, r, fragments.RepoCollaborators(view.RepoCollaboratorsFragData{
			Owner:    owner,
			RepoName: repoName,
			RepoID:   repo.ID,
			Collabs:  collabs,
			CanManage: true,
		}))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

type createOrgRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

type addOrgMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type createOrgRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	AddReadme   bool   `json:"add_readme"`
	Gitignore   string `json:"gitignore"`
	License     string `json:"license"`
}

func (h *Handler) CreateOrg(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	org, err := h.Services.Org.Create(r.Context(), claims.UserID, req.Name, req.DisplayName, req.Description)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (h *Handler) GetOrg(w http.ResponseWriter, r *http.Request) {
	orgName := chi.URLParam(r, "org")
	org, err := h.Services.Org.Get(r.Context(), orgName)
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (h *Handler) ListOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgName := chi.URLParam(r, "org")
	org, err := h.Services.Org.Get(r.Context(), orgName)
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return
	}
	members, err := h.Services.Org.ListMembers(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	if members == nil {
		members = []model.OrgMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *Handler) AddOrgMember(w http.ResponseWriter, r *http.Request) {
	orgName := chi.URLParam(r, "org")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req addOrgMemberRequest
	if err := r.ParseForm(); err == nil && r.FormValue("username") != "" {
		req.Username = r.FormValue("username")
		req.Role = r.FormValue("role")
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if req.Role == "" {
		req.Role = string(model.OrgRoleMember)
	}

	org, err := h.Services.Org.Get(r.Context(), orgName)
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return
	}

	targetUser, err := h.Services.User.GetByUsername(r.Context(), req.Username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if err := h.Services.Org.AddMember(r.Context(), org.ID, claims.UserID, targetUser.ID, model.OrgRole(req.Role)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	members, _ := h.Services.Org.ListMembers(r.Context(), org.ID)
	if members == nil {
		members = []model.OrgMember{}
	}

	canManage := h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID)
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.OrgMembers(view.OrgMembersFragData{
			OrgName:   orgName,
			Members:   members,
			CanManage: canManage,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	orgName := chi.URLParam(r, "org")
	targetUsername := chi.URLParam(r, "username")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	org, err := h.Services.Org.Get(r.Context(), orgName)
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return
	}

	targetUser, err := h.Services.User.GetByUsername(r.Context(), targetUsername)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if err := h.Services.Org.RemoveMember(r.Context(), org.ID, claims.UserID, targetUser.ID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	members, _ := h.Services.Org.ListMembers(r.Context(), org.ID)
	if members == nil {
		members = []model.OrgMember{}
	}

	canManage := h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID)
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.OrgMembers(view.OrgMembersFragData{
			OrgName:   orgName,
			Members:   members,
			CanManage: canManage,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TransferOrg(w http.ResponseWriter, r *http.Request) {
	orgName := chi.URLParam(r, "org")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	org, err := h.Services.Org.Get(r.Context(), orgName)
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return
	}

	newOwner := r.FormValue("new_owner")
	if newOwner == "" {
		writeError(w, http.StatusBadRequest, "new_owner is required")
		return
	}

	if err := h.Services.Org.TransferOrg(r.Context(), org.ID, claims.UserID, newOwner); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "transfer failed")
		return
	}

	http.Redirect(w, r, "/orgs/"+orgName+"/settings", http.StatusSeeOther)
}

func (h *Handler) CreateOrgRepo(w http.ResponseWriter, r *http.Request) {
	orgName := chi.URLParam(r, "org")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createOrgRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	org, err := h.Services.Org.Get(r.Context(), orgName)
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return
	}

	repo, err := h.Services.Org.CreateRepo(r.Context(), org.ID, claims.UserID, req.Name, req.Description, req.Private, service.RepoInitOptions{
		AddREADME: req.AddReadme,
		Gitignore: req.Gitignore,
		License:   req.License,
	})
	if err != nil {
		slog.Error("failed to create org repo", "org", orgName, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "failed to create repository")
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

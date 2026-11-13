package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (h *Handler) ListTeamRepos(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	grants, err := h.Services.Team.ListRepoGrants(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"))
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	if grants == nil {
		grants = []model.TeamRepoGrant{}
	}
	writeJSON(w, http.StatusOK, grants)
}

// GrantTeamRepo gives the team a role on one of the org's repositories, or changes the role it holds.
func (h *Handler) GrantTeamRepo(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if !h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID) {
		writeTeamError(w, r, "", service.ErrTeamOwnerOnly)
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), org.Name, r.FormValue("repo"))
	if err != nil {
		if r.Header.Get("HX-Request") == "true" {
			renderFormError(w, teamRepoFormError, "No repository of this organization has that name.")
			return
		}
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	t, err := h.Services.Team.SetRepoGrant(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"), repo.ID, r.FormValue("role"))
	if err != nil {
		writeTeamError(w, r, teamRepoFormError, err)
		return
	}
	h.auditTeam(r, claims, org, model.AuditActionTeamRepoGrant,
		map[string]any{"team": t.Slug, "repo": repo.Name, "role": r.FormValue("role")})
	if r.Header.Get("HX-Request") == "true" {
		h.renderTeamRepos(w, r, org, claims.UserID, t.Slug)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handler) RevokeTeamRepo(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	if !h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID) {
		writeTeamError(w, r, "", service.ErrTeamOwnerOnly)
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), org.Name, chi.URLParam(r, "repo"))
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	t, err := h.Services.Team.RemoveRepoGrant(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"), repo.ID)
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	h.auditTeam(r, claims, org, model.AuditActionTeamRepoRevoke, map[string]any{"team": t.Slug, "repo": repo.Name})
	if r.Header.Get("HX-Request") == "true" {
		h.renderTeamRepos(w, r, org, claims.UserID, t.Slug)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

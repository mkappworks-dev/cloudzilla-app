package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

const (
	teamCreateFormError = "#team-create-form-error"
	teamEditFormError   = "#team-edit-form-error"
	teamMemberFormError = "#team-member-form-error"
)

// orgForTeams loads the {org} and the caller, answering 401 or 404 itself otherwise.
func (h *Handler) orgForTeams(w http.ResponseWriter, r *http.Request) (*model.Organization, middleware.Claims, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, middleware.Claims{}, false
	}
	org, err := h.Services.Org.Get(r.Context(), chi.URLParam(r, "org"))
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return nil, middleware.Claims{}, false
	}
	return org, claims, true
}

// writeTeamError answers a refused team request: a form error for HTMX when slot is set, JSON otherwise.
func writeTeamError(w http.ResponseWriter, r *http.Request, slot string, err error) {
	status, msg := http.StatusInternalServerError, "internal server error"
	switch {
	case errors.Is(err, service.ErrTeamNotFound), errors.Is(err, sql.ErrNoRows):
		status, msg = http.StatusNotFound, "team not found"
	case errors.Is(err, service.ErrTeamOwnerOnly):
		status, msg = http.StatusForbidden, err.Error()
	case errors.Is(err, service.ErrTeamSlugTaken):
		status, msg = http.StatusUnprocessableEntity, "A team with that name already exists."
	case errors.Is(err, service.ErrNotOrgMember):
		status, msg = http.StatusUnprocessableEntity, "Only members of the organization can join a team."
	case errors.Is(err, service.ErrInvalidTeamName):
		status, msg = http.StatusUnprocessableEntity, err.Error()
	default:
		slog.Error("team request failed", "error", err)
	}
	if slot != "" && r.Header.Get("HX-Request") == "true" && status == http.StatusUnprocessableEntity {
		renderFormError(w, slot, msg)
		return
	}
	writeError(w, status, msg)
}

func (h *Handler) auditTeam(r *http.Request, claims middleware.Claims, org *model.Organization, action string, details map[string]any) {
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username,
		action, model.AuditTargetOrg, org.ID, org.Name, details)
}

func (h *Handler) renderOrgTeams(w http.ResponseWriter, r *http.Request, org *model.Organization) {
	teams, _ := h.Services.Team.List(r.Context(), org.ID, orgViewerID(r))
	h.render(w, r, fragments.OrgTeams(view.OrgTeamsFragData{OrgName: org.Name, Teams: teams}))
}

func orgViewerID(r *http.Request) int64 {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		return claims.UserID
	}
	return 0
}

func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	teams, err := h.Services.Team.List(r.Context(), org.ID, claims.UserID)
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	if teams == nil {
		teams = []model.Team{}
	}
	writeJSON(w, http.StatusOK, teams)
}

func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	t, err := h.Services.Team.Create(r.Context(), org.ID, claims.UserID, r.FormValue("name"), r.FormValue("description"))
	if err != nil {
		writeTeamError(w, r, teamCreateFormError, err)
		return
	}
	h.auditTeam(r, claims, org, model.AuditActionTeamCreate, map[string]any{"team": t.Slug})
	if r.Header.Get("HX-Request") == "true" {
		h.renderOrgTeams(w, r, org)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	oldSlug := chi.URLParam(r, "slug")
	t, err := h.Services.Team.Update(r.Context(), org.ID, claims.UserID, oldSlug, r.FormValue("name"), r.FormValue("description"))
	if err != nil {
		writeTeamError(w, r, teamEditFormError, err)
		return
	}
	h.auditTeam(r, claims, org, model.AuditActionTeamUpdate, map[string]any{"team": t.Slug, "was": oldSlug})
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/orgs/"+org.Name+"/teams/"+t.Slug)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	t, err := h.Services.Team.Delete(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"))
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	h.auditTeam(r, claims, org, model.AuditActionTeamDelete, map[string]any{"team": t.Slug})
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/orgs/"+org.Name+"/settings#teams")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListTeamMembers(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	members, err := h.Services.Team.ListMembers(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"))
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	if members == nil {
		members = []model.TeamMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *Handler) AddTeamMember(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	target, err := h.Services.User.GetByUsername(r.Context(), r.FormValue("username"))
	if err != nil {
		if r.Header.Get("HX-Request") == "true" {
			renderFormError(w, teamMemberFormError, "No user has that username.")
			return
		}
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	t, err := h.Services.Team.AddMember(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"), target.ID)
	if err != nil {
		writeTeamError(w, r, teamMemberFormError, err)
		return
	}
	h.auditTeam(r, claims, org, model.AuditActionTeamMemberAdd, map[string]any{"team": t.Slug, "member": target.Username})
	if r.Header.Get("HX-Request") == "true" {
		h.renderTeamMembers(w, r, org, claims, t.Slug)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handler) RemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	org, claims, ok := h.orgForTeams(w, r)
	if !ok {
		return
	}
	target, err := h.Services.User.GetByUsername(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	t, err := h.Services.Team.RemoveMember(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"), target.ID)
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	h.auditTeam(r, claims, org, model.AuditActionTeamMemberRemove, map[string]any{"team": t.Slug, "member": target.Username})
	if r.Header.Get("HX-Request") == "true" {
		h.renderTeamMembers(w, r, org, claims, t.Slug)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

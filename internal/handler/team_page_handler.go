package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// teamMembersData lists a team's members and, for an owner, the org members who can still be added.
func (h *Handler) teamMembersData(r *http.Request, org *model.Organization, team *model.Team, viewerID int64) (view.TeamMembersFragData, []string) {
	ctx := r.Context()
	members, err := h.Services.Team.ListMembers(ctx, org.ID, viewerID, team.Slug)
	if err != nil {
		slog.Warn("team: failed to load members", "org", org.Name, "team", team.Slug, "error", err)
	}
	canManage := h.Services.Org.IsOwner(ctx, org.ID, viewerID)
	data := view.TeamMembersFragData{OrgName: org.Name, TeamSlug: team.Slug, Members: members, CanManage: canManage}
	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.Username)
	}
	if canManage {
		inTeam := make(map[int64]bool, len(members))
		for _, m := range members {
			inTeam[m.UserID] = true
		}
		orgMembers, _ := h.Services.Org.ListMembers(ctx, org.ID)
		for _, m := range orgMembers {
			if !inTeam[m.UserID] {
				data.Candidates = append(data.Candidates, m.Username)
				names = append(names, m.Username)
			}
		}
	}
	return data, names
}

// teamReposData lists an owner's view of a team's grants and the org repositories it could still be given.
func (h *Handler) teamReposData(r *http.Request, org *model.Organization, team *model.Team, viewerID int64) view.TeamReposFragData {
	ctx := r.Context()
	data := view.TeamReposFragData{OrgName: org.Name, TeamSlug: team.Slug}
	grants, err := h.Services.Team.ListRepoGrants(ctx, org.ID, viewerID, team.Slug)
	if err != nil {
		slog.Warn("team: failed to load repo grants", "org", org.Name, "team", team.Slug, "error", err)
	}
	data.Grants = grants
	granted := make(map[int64]bool, len(grants))
	for _, g := range grants {
		granted[g.RepoID] = true
	}
	repos, _ := h.Services.Org.ListRepos(ctx, org.ID)
	for _, repo := range repos {
		if !granted[repo.ID] {
			data.Candidates = append(data.Candidates, repo.Name)
		}
	}
	return data
}

func (h *Handler) renderTeamRepos(w http.ResponseWriter, r *http.Request, org *model.Organization, viewerID int64, slug string) {
	team, err := h.Services.Team.Get(r.Context(), org.ID, viewerID, slug)
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	h.render(w, r, fragments.TeamRepos(h.teamReposData(r, org, team, viewerID)))
}

// teamGrantsOf lists the team lines of an org repo's collaborator section; the caller has already checked CanManage.
func (h *Handler) teamGrantsOf(ctx context.Context, repo *model.Repository) []model.TeamRepoGrant {
	if repo.OrgID == 0 {
		return nil
	}
	grants, err := h.Services.Team.ListGrantsByRepo(ctx, repo.ID)
	if err != nil {
		slog.Warn("repo settings: failed to load team grants", "repo_id", repo.ID, "error", err)
	}
	return grants
}

func (h *Handler) renderTeamMembers(w http.ResponseWriter, r *http.Request, org *model.Organization, claims middleware.Claims, slug string) {
	team, err := h.Services.Team.Get(r.Context(), org.ID, claims.UserID, slug)
	if err != nil {
		writeTeamError(w, r, "", err)
		return
	}
	data, names := h.teamMembersData(r, org, team, claims.UserID)
	h.render(w, h.withAvatars(r, names...), fragments.TeamMembers(data))
}

// PageOrgTeam renders a team to any member of its organization; outsiders get a 404, like a missing team.
func (h *Handler) PageOrgTeam(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	org, err := h.Services.Org.Get(r.Context(), chi.URLParam(r, "org"))
	if err != nil {
		h.NotFound(w, r)
		return
	}
	team, err := h.Services.Team.Get(r.Context(), org.ID, claims.UserID, chi.URLParam(r, "slug"))
	if err != nil {
		h.NotFound(w, r)
		return
	}
	teams, err := h.Services.Team.List(r.Context(), org.ID, claims.UserID)
	if err != nil {
		slog.Warn("team: failed to list teams", "org", org.Name, "error", err)
	}
	members, names := h.teamMembersData(r, org, team, claims.UserID)

	base := basePage(r, h.Services)
	base.OwnerContext = org.Name
	h.render(w, h.withAvatars(r, names...), pages.OrgTeam(view.OrgTeamData{
		BasePage:  base,
		Org:       *org,
		Team:      *team,
		Teams:     teams,
		Members:   members,
		Repos:     h.teamReposData(r, org, team, claims.UserID),
		CanManage: members.CanManage,
	}))
}

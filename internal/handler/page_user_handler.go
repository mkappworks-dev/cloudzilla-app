package handler

import (
	"html/template"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// PageUser renders the user or organization profile page at /:username.
func (h *Handler) PageUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "owner")

	user, err := h.Services.User.GetByUsername(r.Context(), username)
	if err != nil {
		// Not a user — try org
		org, orgErr := h.Services.Org.Get(r.Context(), username)
		if orgErr != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.pageOrgProfile(w, r, org)
		return
	}

	repos, err := h.Services.Repo.ListByOwner(r.Context(), username)
	if err != nil {
		slog.Warn("user profile: failed to load repositories", "username", username, "error", err)
		repos = []model.Repository{}
	}
	if repos == nil {
		repos = []model.Repository{}
	}

	activity, err := h.Services.Event.UserActivity(r.Context(), username, 1, 15)
	if err != nil {
		slog.Warn("user activity: failed to load events", "username", username, "error", err)
		activity = []model.Event{}
	}
	if activity == nil {
		activity = []model.Event{}
	}
	var profileReadme template.HTML
	for _, repo := range repos {
		if repo.Name == user.Username && !repo.Private {
			profileReadme = h.Services.Code.GetProfileReadme(user.Username, user.Username, repo.DefaultBranch)
			break
		}
	}

	h.render(w, r, pages.User(view.UserData{
		BasePage:       basePage(r, h.Services),
		User:           *user,
		Repos:          repos,
		RecentActivity: activity,
		ProfileReadme:  profileReadme,
	}))
}

func (h *Handler) pageOrgProfile(w http.ResponseWriter, r *http.Request, org *model.Organization) {
	repos, _ := h.Services.Org.ListRepos(r.Context(), org.ID)
	members, _ := h.Services.Org.ListMembers(r.Context(), org.ID)
	if repos == nil {
		repos = []model.Repository{}
	}
	if members == nil {
		members = []model.OrgMember{}
	}

	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID)
	}

	h.render(w, r, pages.Org(view.OrgData{
		BasePage:  basePage(r, h.Services),
		Org:       *org,
		Repos:     repos,
		Members:   members,
		CanManage: canManage,
	}))
}

// PageOrgSettings renders the organization settings page for org owners.
func (h *Handler) PageOrgSettings(w http.ResponseWriter, r *http.Request) {
	orgName := chi.URLParam(r, "org")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	org, err := h.Services.Org.Get(r.Context(), orgName)
	if err != nil {
		http.Error(w, "org not found", http.StatusNotFound)
		return
	}

	if !h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	members, _ := h.Services.Org.ListMembers(r.Context(), org.ID)
	if members == nil {
		members = []model.OrgMember{}
	}

	h.render(w, r, pages.OrgSettings(view.OrgSettingsData{
		BasePage: basePage(r, h.Services),
		Org:      *org,
		Members:  members,
	}))
}

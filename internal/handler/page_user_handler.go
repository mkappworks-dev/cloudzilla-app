package handler

import (
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageUser renders the user or organization profile page at /:username.
func (h *Handler) PageUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "owner")

	user, err := h.Services.User.GetByUsername(r.Context(), username)
	if err != nil {
		// Not a user — try org
		org, orgErr := h.Services.Org.Get(r.Context(), username)
		if orgErr != nil {
			h.NotFound(w, r)
			return
		}
		h.pageOrgProfile(w, r, org)
		return
	}

	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	repos, err := h.Services.Repo.ListByOwnerVisibleTo(r.Context(), username, viewerID)
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

	isOwn := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		isOwn = claims.UserID == user.ID
	}

	tab := r.URL.Query().Get("tab")
	if tab != "repositories" {
		tab = "overview"
	}

	var pinned []components.PinnedRepoData
	if tab == "overview" {
		ids, err := h.Services.User.PinnedRepoIDs(r.Context(), user.ID)
		if err != nil {
			slog.Warn("user profile: failed to load pinned repositories", "username", username, "error", err)
			ids = nil
		}
		for _, rid := range ids {
			rp, err := h.Services.Repo.GetByID(r.Context(), rid)
			if err != nil || rp == nil {
				continue
			}
			lang, _ := h.Services.Language.TopLanguageFor(r.Context(), rp.OwnerName, rp.Name, rp.DefaultBranch)
			stars, _ := h.Services.Star.GetStarCount(r.Context(), rp.ID)
			pinned = append(pinned, components.PinnedRepoData{
				OwnerName:     rp.OwnerName,
				Name:          rp.Name,
				Description:   rp.Description,
				Language:      lang,
				LanguageColor: components.LangColor(lang),
				Stars:         stars,
			})
		}
	}

	heatmap, err := h.Services.CommitStats.LookbackForUser(r.Context(), user.ID, 365)
	if err != nil {
		slog.Warn("user profile: failed to load commit heatmap", "username", username, "error", err)
		heatmap = nil
	}
	if heatmap == nil {
		heatmap = map[time.Time]int{}
	}

	langPcts, err := h.Services.Language.AggregateForUser(r.Context(), user.ID, 5)
	if err != nil {
		slog.Warn("user profile: failed to load language stats", "username", username, "error", err)
		langPcts = nil
	}
	topLangs := make([]components.LangBarItem, 0, len(langPcts))
	for _, p := range langPcts {
		topLangs = append(topLangs, components.LangBarItem{
			Name:    p.Name,
			Percent: p.Percent,
			Color:   components.LangColor(p.Name),
		})
	}

	orgs, err := h.Services.Org.ListMembershipsForUser(r.Context(), user.ID)
	if err != nil {
		slog.Warn("user profile: failed to load organization memberships", "username", username, "error", err)
		orgs = nil
	}
	if orgs == nil {
		orgs = []service.OrgMembership{}
	}

	h.render(w, r, pages.User(view.UserData{
		BasePage:       basePage(r, h.Services),
		User:           *user,
		Repos:          repos,
		RecentActivity: activity,
		ProfileReadme:  profileReadme,
		IsOwnProfile:   isOwn,
		Tab:            tab,
		PinnedRepos:    pinned,
		Heatmap:        heatmap,
		TopLangs:       topLangs,
		Orgs:           orgs,
	}))
}

func (h *Handler) pageOrgProfile(w http.ResponseWriter, r *http.Request, org *model.Organization) {
	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	repos, _ := h.Services.Org.ListReposVisibleTo(r.Context(), org.ID, viewerID)
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
		h.NotFound(w, r)
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

package handler

import (
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
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
	var viewerUserID int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
		viewerUserID = claims.UserID
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

	data := view.UserData{
		BasePage:       basePage(r, h.Services),
		User:           *user,
		Repos:          repos,
		RecentActivity: activity,
		ProfileReadme:  profileReadme,
	}

	if r.URL.Query().Get("tab") == "repositories" {
		data = h.buildRepoTabData(r, data, repos, user.ID, viewerUserID)
	}

	h.render(w, r, pages.User(data))
}

func (h *Handler) buildRepoTabData(r *http.Request, data view.UserData, allRepos []model.Repository, profileOwnerID, viewerUserID int64) view.UserData {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	repoType := r.URL.Query().Get("type")
	langFilter := r.URL.Query().Get("language")
	statusFilter := r.URL.Query().Get("status")

	// Collect language chips from the unfiltered list.
	seen := map[string]struct{}{}
	var languages []string
	for _, repo := range allRepos {
		if repo.PrimaryLanguage != nil && *repo.PrimaryLanguage != "" {
			lang := *repo.PrimaryLanguage
			if _, ok := seen[lang]; !ok {
				seen[lang] = struct{}{}
				languages = append(languages, lang)
			}
		}
	}
	sort.Strings(languages)

	// Batch-resolve viewer roles.
	roleMap := make(map[int64]string)
	if viewerUserID != 0 {
		perms, err := h.Services.Repo.ListPermissionsByUser(r.Context(), viewerUserID)
		if err != nil {
			slog.Warn("user profile repos tab: failed to load viewer permissions", "viewer_id", viewerUserID, "error", err)
		}
		for _, p := range perms {
			roleMap[p.RepoID] = string(p.Role)
		}
	}

	// Apply filters.
	filtered := make([]model.Repository, 0, len(allRepos))
	for _, repo := range allRepos {
		if q != "" {
			lq := strings.ToLower(q)
			if !strings.Contains(strings.ToLower(repo.Name), lq) && !strings.Contains(strings.ToLower(repo.Description), lq) {
				continue
			}
		}
		switch repoType {
		case "sources":
			if repo.ForkOfID != nil || repo.IsTemplate {
				continue
			}
		case "forks":
			if repo.ForkOfID == nil {
				continue
			}
		case "templates":
			if !repo.IsTemplate {
				continue
			}
		}
		switch statusFilter {
		case "public":
			if repo.Private {
				continue
			}
		case "private":
			if !repo.Private {
				continue
			}
		}
		if langFilter != "" {
			if repo.PrimaryLanguage == nil || *repo.PrimaryLanguage != langFilter {
				continue
			}
		}
		filtered = append(filtered, repo)
	}

	// Inject owner role: viewer == profile owner → "owner".
	for _, repo := range filtered {
		if repo.OwnerID == viewerUserID {
			roleMap[repo.ID] = "owner"
		}
	}

	data.RepoTabRepos = filtered
	data.RepoTabRoles = roleMap
	data.RepoTabLanguages = languages
	data.RepoTabActiveQuery = q
	data.RepoTabActiveType = repoType
	data.RepoTabActiveLanguage = langFilter
	data.RepoTabActiveStatus = statusFilter
	return data
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

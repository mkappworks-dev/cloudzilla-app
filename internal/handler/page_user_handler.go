package handler

import (
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
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
			if !h.Services.Repo.CanRead(r.Context(), rp, viewerID) {
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

	data := view.UserData{
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
	}

	if tab == "repositories" {
		data = h.buildRepoTabData(r, data, repos, user.ID, viewerUserID)
	}

	h.render(w, r, pages.User(data))
}

func (h *Handler) buildRepoTabData(r *http.Request, data view.UserData, allRepos []model.Repository, profileOwnerID, viewerUserID int64) view.UserData {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	repoType := r.URL.Query().Get("type")
	langFilter := r.URL.Query().Get("language")
	statusFilter := r.URL.Query().Get("status")

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
			if repo.IsFork || repo.IsTemplate {
				continue
			}
		case "forks":
			if !repo.IsFork {
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

	repoIDs := make([]int64, len(filtered))
	for i, repo := range filtered {
		repoIDs[i] = repo.ID
	}

	starCounts, err := h.Services.Star.CountByRepoIDs(r.Context(), repoIDs)
	if err != nil {
		slog.Warn("user profile repos tab: failed to load star counts", "error", err)
		starCounts = map[int64]int{}
	}

	topicMap, err := h.Services.Topic.ListByRepoIDs(r.Context(), repoIDs)
	if err != nil {
		slog.Warn("user profile repos tab: failed to load topics", "error", err)
		topicMap = map[int64][]model.Topic{}
	}

	data.RepoTabRepos = filtered
	data.RepoTabRoles = roleMap
	data.RepoTabLanguages = languages
	data.RepoTabStars = starCounts
	data.RepoTabTopics = topicMap
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
	repos, err := h.Services.Org.ListReposVisibleTo(r.Context(), org.ID, viewerID)
	if err != nil {
		slog.Warn("org profile: failed to load repositories", "org", org.Name, "error", err)
		repos = nil
	}
	members, err := h.Services.Org.ListMembers(r.Context(), org.ID)
	if err != nil {
		slog.Warn("org profile: failed to load members", "org", org.Name, "error", err)
		members = nil
	}
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

// PageOrganizations renders the organizations listing page at /settings/organizations.
func (h *Handler) PageOrganizations(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	memberships, err := h.Services.Org.ListMembershipsForUser(r.Context(), claims.UserID)
	if err != nil {
		slog.Warn("organizations: failed to load memberships", "user_id", claims.UserID, "error", err)
		memberships = nil
	}

	entries := make([]view.OrgListEntry, 0, len(memberships))
	for _, m := range memberships {
		count, err := h.Services.Org.CountMembers(r.Context(), m.Org.ID)
		if err != nil {
			slog.Warn("organizations: failed to count members", "org_id", m.Org.ID, "error", err)
			count = 0
		}
		entries = append(entries, view.OrgListEntry{
			Org:         m.Org,
			Role:        m.Role,
			MemberCount: count,
		})
	}

	h.render(w, r, pages.Organizations(view.OrgListData{
		BasePage: basePage(r, h.Services),
		Entries:  entries,
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

	members, err := h.Services.Org.ListMembers(r.Context(), org.ID)
	if err != nil {
		slog.Warn("org settings: failed to load members", "org", org.Name, "error", err)
		members = nil
	}
	if members == nil {
		members = []model.OrgMember{}
	}

	h.render(w, r, pages.OrgSettings(view.OrgSettingsData{
		BasePage: basePage(r, h.Services),
		Org:      *org,
		Members:  members,
	}))
}

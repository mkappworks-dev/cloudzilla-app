package handler

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
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

const profileTabPageSize = 20

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
	var profileReadmeRaw string
	var hasProfileRepo bool
	var profileRepoDefaultBranch string
	for _, repo := range repos {
		if repo.Name == user.Username && !repo.Private {
			hasProfileRepo = true
			profileRepoDefaultBranch = repo.DefaultBranch
			profileReadme = h.Services.Code.GetProfileReadme(user.Username, user.Username, repo.DefaultBranch)
			if raw, err := h.Services.Code.GetProfileReadmeRaw(user.Username, user.Username, repo.DefaultBranch); err == nil {
				profileReadmeRaw = raw
			} else {
				slog.Warn("user profile: failed to load raw README", "username", username, "error", err)
			}
			break
		}
	}

	isOwn := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		isOwn = claims.UserID == user.ID
	}

	tab := r.URL.Query().Get("tab")
	switch tab {
	case "repositories", "stars", "gists":
	default:
		tab = "overview"
	}

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	var pinned []components.PinnedRepoData
	pinnedIDs := map[int64]bool{}
	if tab == "overview" {
		ids, err := h.Services.User.PinnedRepoIDs(r.Context(), user.ID)
		if err != nil {
			slog.Warn("user profile: failed to load pinned repositories", "username", username, "error", err)
			ids = nil
		}
		for _, rid := range ids {
			pinnedIDs[rid] = true
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

	starsTotal, err := h.Services.Star.CountByUser(r.Context(), user.ID)
	if err != nil {
		slog.Warn("user profile: failed to count starred repos", "username", username, "error", err)
		starsTotal = 0
	}

	var gistsTotal int
	if isOwn {
		gistsTotal, err = h.Services.Gist.CountByUser(r.Context(), user.ID)
	} else {
		gistsTotal, err = h.Services.Gist.CountPublicByOwner(r.Context(), user.ID)
	}
	if err != nil {
		slog.Warn("user profile: failed to count gists", "username", username, "error", err)
		gistsTotal = 0
	}

	data := view.UserData{
		BasePage:                 basePage(r, h.Services),
		User:                     *user,
		Repos:                    repos,
		RecentActivity:           activity,
		ProfileReadme:            profileReadme,
		ProfileReadmeRaw:         profileReadmeRaw,
		ReadmeError:              r.URL.Query().Get("readme_error"),
		IsOwnProfile:             isOwn,
		Tab:                      tab,
		PinnedRepos:              pinned,
		PinnedRepoIDs:            pinnedIDs,
		Heatmap:                  heatmap,
		TopLangs:                 topLangs,
		Orgs:                     orgs,
		HasProfileRepo:           hasProfileRepo,
		ProfileRepoDefaultBranch: profileRepoDefaultBranch,
		ReposTotal:               len(repos),
		StarsTotal:               starsTotal,
		GistsTotal:               gistsTotal,
	}
	data.OwnerContext = user.Username

	switch tab {
	case "repositories":
		data = h.buildRepoTabData(r, data, repos, user.ID, viewerUserID, page)
	case "stars":
		data = h.buildStarsTabData(r, data, username, page)
	case "gists":
		data = h.buildGistsTabData(r, data, user.ID, isOwn, page, gistsTotal)
	}

	h.render(w, r, pages.User(data))
}

func (h *Handler) buildRepoTabData(r *http.Request, data view.UserData, allRepos []model.Repository, profileOwnerID, viewerUserID int64, page int) view.UserData {
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

	total := len(filtered)
	totalPages := (total + profileTabPageSize - 1) / profileTabPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * profileTabPageSize
	end := start + profileTabPageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	paged := filtered[start:end]

	repoIDs := make([]int64, len(paged))
	for i, repo := range paged {
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

	data.RepoTabRepos = paged
	data.RepoTabRoles = roleMap
	data.RepoTabLanguages = languages
	data.RepoTabStars = starCounts
	data.RepoTabTopics = topicMap
	data.RepoTabActiveQuery = q
	data.RepoTabActiveType = repoType
	data.RepoTabActiveLanguage = langFilter
	data.RepoTabActiveStatus = statusFilter
	data.RepoTabPage = page
	data.RepoTabTotalPages = totalPages
	return data
}

func (h *Handler) buildStarsTabData(r *http.Request, data view.UserData, username string, page int) view.UserData {
	all, err := h.Services.Star.ListByUser(r.Context(), username)
	if err != nil {
		slog.Warn("user profile stars tab: failed to load starred repos", "username", username, "error", err)
		all = nil
	}
	total := len(all)
	totalPages := (total + profileTabPageSize - 1) / profileTabPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * profileTabPageSize
	end := start + profileTabPageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	data.StarredRepos = all[start:end]
	data.StarsTabPage = page
	data.StarsTabTotalPages = totalPages
	return data
}

func (h *Handler) buildGistsTabData(r *http.Request, data view.UserData, ownerID int64, isOwner bool, page, total int) view.UserData {
	totalPages := (total + profileTabPageSize - 1) / profileTabPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var gists []model.Gist
	var err error
	if isOwner {
		gists, err = h.Services.Gist.ListByOwner(r.Context(), ownerID, page, profileTabPageSize)
	} else {
		gists, err = h.Services.Gist.ListPublicByOwner(r.Context(), ownerID, page, profileTabPageSize)
	}
	if err != nil {
		slog.Warn("user profile gists tab: failed to load gists", "owner_id", ownerID, "error", err)
		gists = nil
	}

	ids := make([]string, 0, len(gists))
	for _, g := range gists {
		ids = append(ids, g.ID)
	}
	filenamesByGist, err := h.Services.Gist.LoadFilenames(r.Context(), ids)
	if err != nil {
		slog.Warn("user profile gists tab: failed to load filenames", "error", err)
		filenamesByGist = map[string][]string{}
	}

	items := make([]view.GistTabItem, 0, len(gists))
	for _, g := range gists {
		files := filenamesByGist[g.ID]
		label, chipClass := gistLanguage(files)
		items = append(items, view.GistTabItem{
			Gist:          g,
			FileCount:     len(files),
			LanguageLabel: label,
			LanguageClass: chipClass,
		})
	}

	data.GistsTabItems = items
	data.GistsTabPage = page
	data.GistsTabTotalPages = totalPages
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
	var viewerRole *model.OrgRole
	var viewerJoined *time.Time
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID)
		for _, m := range members {
			if m.UserID == claims.UserID {
				role := m.Role
				viewerRole = &role
				joined := m.CreatedAt
				viewerJoined = &joined
				break
			}
		}
	}

	memberCount := len(members)
	showAllRepos := r.URL.Query().Get("tab") == "repositories"

	var pinned, recent []components.PinnedRepoData
	if showAllRepos {
		recent = buildOrgRecent(r.Context(), h, org.Name, repos, nil, len(repos))
	} else {
		// Pinned repos: until we have org-level pin storage, surface the four
		// most-starred public repos so the section still feels curated.
		pinned = buildOrgPinned(r.Context(), h, org.Name, repos, 4)

		// Recently updated repos for the "Recently updated" list — exclude the
		// ones we already showed as pinned.
		pinnedKeys := map[string]struct{}{}
		for _, p := range pinned {
			pinnedKeys[p.OwnerName+"/"+p.Name] = struct{}{}
		}
		recent = buildOrgRecent(r.Context(), h, org.Name, repos, pinnedKeys, 4)
	}

	// Top languages aggregated from each repo's primary_language column.
	topLangs := aggregateOrgLanguages(repos, 5)

	// Profile README — render the README.md from the repo named after the
	// org, mirroring the user-profile convention.
	var profileReadme template.HTML
	for _, repo := range repos {
		if !showAllRepos && repo.Name == org.Name && !repo.Private {
			profileReadme = h.Services.Code.GetProfileReadme(org.Name, org.Name, repo.DefaultBranch)
			break
		}
	}

	base := basePage(r, h.Services)
	base.OwnerContext = org.Name
	h.render(w, r, pages.Org(view.OrgData{
		BasePage:      base,
		Org:           *org,
		Repos:         repos,
		Members:       members,
		MemberCount:   memberCount,
		CanManage:     canManage,
		ProfileReadme: profileReadme,
		PinnedRepos:   pinned,
		RecentRepos:   recent,
		ShowAllRepos:  showAllRepos,
		TopLangs:      topLangs,
		ViewerRole:    viewerRole,
		ViewerJoined:  viewerJoined,
	}))
}

// buildOrgPinned returns up to `limit` PinnedRepoData built from the org's
// most-starred public repos. Stars and primary language are fetched per repo.
func buildOrgPinned(ctx context.Context, h *Handler, orgName string, repos []model.Repository, limit int) []components.PinnedRepoData {
	type scored struct {
		repo  model.Repository
		stars int
	}
	candidates := make([]scored, 0, len(repos))
	for _, repo := range repos {
		if repo.Private {
			continue
		}
		stars, _ := h.Services.Star.GetStarCount(ctx, repo.ID)
		candidates = append(candidates, scored{repo: repo, stars: stars})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].stars != candidates[j].stars {
			return candidates[i].stars > candidates[j].stars
		}
		return candidates[i].repo.UpdatedAt.After(candidates[j].repo.UpdatedAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]components.PinnedRepoData, 0, len(candidates))
	for _, c := range candidates {
		lang := ""
		if c.repo.PrimaryLanguage != nil {
			lang = *c.repo.PrimaryLanguage
		}
		out = append(out, components.PinnedRepoData{
			OwnerName:     orgName,
			Name:          c.repo.Name,
			Description:   c.repo.Description,
			Language:      lang,
			LanguageColor: components.LangColor(lang),
			Stars:         c.stars,
		})
	}
	return out
}

// buildOrgRecent returns up to `limit` PinnedRepoData for the org's most
// recently updated repos, skipping any already surfaced in `exclude`.
func buildOrgRecent(ctx context.Context, h *Handler, orgName string, repos []model.Repository, exclude map[string]struct{}, limit int) []components.PinnedRepoData {
	sorted := make([]model.Repository, len(repos))
	copy(sorted, repos)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
	})
	out := make([]components.PinnedRepoData, 0, limit)
	for _, repo := range sorted {
		if _, skip := exclude[orgName+"/"+repo.Name]; skip {
			continue
		}
		stars, _ := h.Services.Star.GetStarCount(ctx, repo.ID)
		lang := ""
		if repo.PrimaryLanguage != nil {
			lang = *repo.PrimaryLanguage
		}
		out = append(out, components.PinnedRepoData{
			OwnerName:     orgName,
			Name:          repo.Name,
			Description:   repo.Description,
			Language:      lang,
			LanguageColor: components.LangColor(lang),
			Stars:         stars,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// aggregateOrgLanguages computes the percentage breakdown of primary languages
// across the org's repos. Uses each repo's primary_language column rather than
// per-file byte counts so the result is one cheap query slice.
func aggregateOrgLanguages(repos []model.Repository, limit int) []components.LangBarItem {
	counts := map[string]int{}
	total := 0
	for _, repo := range repos {
		if repo.PrimaryLanguage == nil || *repo.PrimaryLanguage == "" {
			continue
		}
		counts[*repo.PrimaryLanguage]++
		total++
	}
	if total == 0 {
		return nil
	}
	type kv struct {
		Name  string
		Count int
	}
	pairs := make([]kv, 0, len(counts))
	for name, c := range counts {
		pairs = append(pairs, kv{Name: name, Count: c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count != pairs[j].Count {
			return pairs[i].Count > pairs[j].Count
		}
		return pairs[i].Name < pairs[j].Name
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	out := make([]components.LangBarItem, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, components.LangBarItem{
			Name:    p.Name,
			Percent: int(float64(p.Count) / float64(total) * 100.0),
			Color:   components.LangColor(p.Name),
		})
	}
	return out
}

// PageOrganizations renders the organizations listing page at /organizations.
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

	repos, err := h.Services.Org.ListRepos(r.Context(), org.ID)
	if err != nil {
		slog.Warn("org settings: failed to count repositories", "org", org.Name, "error", err)
		repos = nil
	}

	orgID := org.ID
	auditEntries, _, err := h.Services.AuditLog.List(r.Context(), model.AuditFilter{
		TargetType: model.AuditTargetOrg,
		TargetID:   &orgID,
	}, 1, 25)
	if err != nil {
		slog.Warn("org settings: failed to load audit log", "org", org.Name, "error", err)
		auditEntries = nil
	}

	base := basePage(r, h.Services)
	base.OwnerContext = org.Name
	h.render(w, r, pages.OrgSettings(view.OrgSettingsData{
		BasePage:     base,
		Org:          *org,
		Members:      members,
		MemberCount:  len(members),
		RepoCount:    len(repos),
		AuditEntries: auditEntries,
	}))
}

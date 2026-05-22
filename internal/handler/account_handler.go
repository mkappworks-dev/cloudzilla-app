package handler

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageAccountRepos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	filter := r.URL.Query().Get("filter")
	switch filter {
	case "owned", "collaborator", "forks":
	default:
		filter = "all"
	}
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "name", "stars", "created":
	default:
		sortBy = "updated"
	}
	language := r.URL.Query().Get("language")

	all, err := h.Services.Repo.ListForUser(ctx, claims.UserID, "all")
	if err != nil {
		slog.Error("repos: failed to load repositories", "error", err)
		http.Error(w, "Failed to load repositories", http.StatusInternalServerError)
		return
	}

	seen := map[string]struct{}{}
	var languages []string
	for _, repo := range all {
		if repo.PrimaryLanguage == nil || *repo.PrimaryLanguage == "" {
			continue
		}
		if _, dup := seen[*repo.PrimaryLanguage]; dup {
			continue
		}
		seen[*repo.PrimaryLanguage] = struct{}{}
		languages = append(languages, *repo.PrimaryLanguage)
	}
	sort.Strings(languages)

	typeCounts := map[string]int{"all": len(all)}
	for _, repo := range all {
		if repo.OwnerID == claims.UserID {
			typeCounts["owned"]++
		} else {
			typeCounts["collaborator"]++
		}
		if repo.IsFork {
			typeCounts["forks"]++
		}
	}

	repos := make([]model.Repository, 0, len(all))
	for _, repo := range all {
		switch filter {
		case "owned":
			if repo.OwnerID != claims.UserID {
				continue
			}
		case "collaborator":
			if repo.OwnerID == claims.UserID {
				continue
			}
		case "forks":
			if !repo.IsFork {
				continue
			}
		}
		if language != "" && (repo.PrimaryLanguage == nil || *repo.PrimaryLanguage != language) {
			continue
		}
		repos = append(repos, repo)
	}

	// Star counts are fetched for the whole filtered set so the "stars" sort
	// can order across pages; it is a single batch query regardless of size.
	starIDs := make([]int64, len(repos))
	for i, repo := range repos {
		starIDs[i] = repo.ID
	}
	starCounts := map[int64]int{}
	if len(starIDs) > 0 {
		if counts, cErr := h.Services.Star.CountByRepoIDs(ctx, starIDs); cErr != nil {
			slog.Warn("repos: failed to load star counts; showing 0", "error", cErr)
		} else {
			starCounts = counts
		}
	}

	switch sortBy {
	case "name":
		sort.Slice(repos, func(i, j int) bool {
			return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
		})
	case "created":
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].CreatedAt.After(repos[j].CreatedAt)
		})
	case "stars":
		sort.Slice(repos, func(i, j int) bool {
			return starCounts[repos[i].ID] > starCounts[repos[j].ID]
		})
	}

	const pageSize = 10
	totalPages := (len(repos) + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	page := 1
	if p, perr := strconv.Atoi(r.URL.Query().Get("page")); perr == nil && p > 1 {
		page = p
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > len(repos) {
		end = len(repos)
	}
	repos = repos[start:end]

	repoIDs := make([]int64, len(repos))
	for i, repo := range repos {
		repoIDs[i] = repo.ID
	}

	// Commit counts walk git history per repo, so only do it for the page.
	commitCounts := make(map[int64]int, len(repos))
	for _, repo := range repos {
		if n, cErr := h.Services.Code.CommitCount(repo.OwnerName, repo.Name, repo.DefaultBranch); cErr == nil {
			commitCounts[repo.ID] = n
		}
	}

	repoTopics := map[int64][]model.Topic{}
	if len(repoIDs) > 0 {
		if topics, tErr := h.Services.Topic.ListByRepoIDs(ctx, repoIDs); tErr != nil {
			slog.Warn("repos: failed to load topics", "error", tErr)
		} else {
			repoTopics = topics
		}
	}

	data := view.AccountReposData{
		BasePage:     withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(ctx, claims.UserID)),
		Repos:        repos,
		Filter:       filter,
		TypeCounts:   typeCounts,
		Language:     language,
		Languages:    languages,
		Sort:         sortBy,
		StarCounts:   starCounts,
		CommitCounts: commitCounts,
		Topics:       repoTopics,
		UserID:       claims.UserID,
		Total:        len(all),
		Page:         page,
		TotalPages:   totalPages,
	}
	h.render(w, r, pages.AccountRepos(data))
}

func (h *Handler) PageAccountPulls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	filter := r.URL.Query().Get("filter")
	switch filter {
	case "assigned", "review_requested", "mentioned":
	default:
		filter = "created"
	}
	state := r.URL.Query().Get("state")
	if state != "closed" {
		state = "open"
	}
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "oldest", "updated", "comments":
	default:
		sortBy = "newest"
	}
	pulls, err := h.Services.Pull.ListForUser(ctx, claims.UserID, filter, state)
	if err != nil {
		slog.Error("pulls: failed to load pull requests", "filter", filter, "state", state, "error", err)
		http.Error(w, "Failed to load pull requests", http.StatusInternalServerError)
		return
	}
	pullIDs := make([]int64, len(pulls))
	for i, p := range pulls {
		pullIDs[i] = p.ID
	}
	commentCounts, err := h.Services.Comment.CountByPullIDs(ctx, pullIDs)
	if err != nil {
		slog.Error("pulls: failed to load comment counts", "error", err)
		http.Error(w, "Failed to load pull requests", http.StatusInternalServerError)
		return
	}
	pullLabels, err := h.Services.Label.BatchForPullIDs(ctx, pullIDs)
	if err != nil {
		slog.Error("pulls: failed to load labels", "error", err)
		http.Error(w, "Failed to load pull requests", http.StatusInternalServerError)
		return
	}
	pullCI, err := h.Services.CommitStatus.CountsByPullIDs(ctx, pullIDs)
	if err != nil {
		slog.Warn("pulls: failed to load CI counts", "error", err)
		pullCI = nil
	}
	pullReviewers, err := h.Services.PullReview.ListReviewersByPullIDs(ctx, pullIDs)
	if err != nil {
		slog.Warn("pulls: failed to load reviewers", "error", err)
		pullReviewers = nil
	}
	switch sortBy {
	case "oldest":
		sort.Slice(pulls, func(i, j int) bool { return pulls[i].CreatedAt.Before(pulls[j].CreatedAt) })
	case "updated":
		sort.Slice(pulls, func(i, j int) bool { return pulls[i].UpdatedAt.After(pulls[j].UpdatedAt) })
	case "comments":
		sort.Slice(pulls, func(i, j int) bool { return commentCounts[pulls[i].ID] > commentCounts[pulls[j].ID] })
	default:
		sort.Slice(pulls, func(i, j int) bool { return pulls[i].CreatedAt.After(pulls[j].CreatedAt) })
	}

	tabCounts, err := h.Services.Pull.CountsForUser(ctx, claims.UserID)
	if err != nil {
		slog.Warn("pulls: failed to load tab counts; showing 0", "error", err)
		tabCounts = nil
	}
	data := view.AccountPullsData{
		BasePage:      withAccountSubnav(basePage(r, h.Services), "pulls", h.accountCounts(ctx, claims.UserID)),
		Pulls:         pulls,
		Filter:        filter,
		State:         state,
		Sort:          sortBy,
		Counts:        tabCounts,
		PullComments:  commentCounts,
		PullLabels:    pullLabels,
		PullCI:        pullCI,
		PullReviewers: pullReviewers,
	}
	h.render(w, r, pages.AccountPulls(data))
}

func (h *Handler) PageAccountIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	filter := r.URL.Query().Get("filter")
	switch filter {
	case "created", "mentioned":
	default:
		filter = "assigned"
	}
	state := r.URL.Query().Get("state")
	if state != "closed" {
		state = "open"
	}
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "oldest", "updated", "comments":
	default:
		sortBy = "newest"
	}
	issues, err := h.Services.Issue.ListForUser(ctx, claims.UserID, filter, state)
	if err != nil {
		slog.Error("issues: failed to load issues", "filter", filter, "state", state, "error", err)
		http.Error(w, "Failed to load issues", http.StatusInternalServerError)
		return
	}
	issueIDs := make([]int64, len(issues))
	for i, is := range issues {
		issueIDs[i] = is.ID
	}
	commentCounts, err := h.Services.Comment.CountByIssueIDs(ctx, issueIDs)
	if err != nil {
		slog.Error("issues: failed to load comment counts", "error", err)
		http.Error(w, "Failed to load issues", http.StatusInternalServerError)
		return
	}
	issueLabels, err := h.Services.Label.BatchForIssueIDs(ctx, issueIDs)
	if err != nil {
		slog.Error("issues: failed to load labels", "error", err)
		http.Error(w, "Failed to load issues", http.StatusInternalServerError)
		return
	}
	tabCounts, err := h.Services.Issue.CountsForUser(ctx, claims.UserID)
	if err != nil {
		slog.Warn("issues: failed to load tab counts; showing 0", "error", err)
		tabCounts = nil
	}
	switch sortBy {
	case "oldest":
		sort.Slice(issues, func(i, j int) bool { return issues[i].CreatedAt.Before(issues[j].CreatedAt) })
	case "updated":
		sort.Slice(issues, func(i, j int) bool { return issues[i].UpdatedAt.After(issues[j].UpdatedAt) })
	case "comments":
		sort.Slice(issues, func(i, j int) bool { return commentCounts[issues[i].ID] > commentCounts[issues[j].ID] })
	default:
		sort.Slice(issues, func(i, j int) bool { return issues[i].CreatedAt.After(issues[j].CreatedAt) })
	}

	data := view.AccountIssuesData{
		BasePage:      withAccountSubnav(basePage(r, h.Services), "issues", h.accountCounts(ctx, claims.UserID)),
		Issues:        issues,
		Filter:        filter,
		State:         state,
		Sort:          sortBy,
		IssueComments: commentCounts,
		IssueLabels:   issueLabels,
		Counts:        tabCounts,
	}
	h.render(w, r, pages.AccountIssues(data))
}

func (h *Handler) PageAccountStars(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	stars, err := h.Services.Star.ListByUser(ctx, claims.Username)
	if err != nil {
		slog.Error("stars: failed to load starred repositories", "username", claims.Username, "error", err)
		http.Error(w, "Failed to load starred repositories", http.StatusInternalServerError)
		return
	}
	if stars == nil {
		stars = []model.Repository{}
	}

	seen := map[string]struct{}{}
	var languages []string
	for _, repo := range stars {
		if repo.PrimaryLanguage != nil && *repo.PrimaryLanguage != "" {
			lang := *repo.PrimaryLanguage
			if _, ok := seen[lang]; !ok {
				seen[lang] = struct{}{}
				languages = append(languages, lang)
			}
		}
	}
	sort.Strings(languages)

	langFilter := r.URL.Query().Get("language")
	if langFilter != "" {
		filtered := make([]model.Repository, 0, len(stars))
		for _, repo := range stars {
			if repo.PrimaryLanguage != nil && *repo.PrimaryLanguage == langFilter {
				filtered = append(filtered, repo)
			}
		}
		stars = filtered
	}

	data := view.AccountStarsData{
		BasePage:  basePage(r, h.Services),
		Username:  claims.Username,
		Stars:     stars,
		Language:  langFilter,
		Languages: languages,
	}
	h.render(w, r, pages.AccountStars(data))
}

func (h *Handler) PageAttention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	all, err := h.Services.Attention.ForUser(ctx, claims.UserID)
	if err != nil {
		slog.Error("attention: failed to load items", "user_id", claims.UserID, "error", err)
		http.Error(w, "Failed to load attention items", http.StatusInternalServerError)
		return
	}

	kind := r.URL.Query().Get("kind")
	switch kind {
	case "mentions", "reviews", "assigned":
	default:
		kind = "all"
	}

	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "newest", "oldest":
	default:
		sortBy = "overdue"
	}

	counts := map[string]int{"all": len(all), "mentions": 0, "reviews": 0, "assigned": 0}
	for _, item := range all {
		switch item.Kind {
		case service.AttentionIssueAssigned, service.AttentionPRAssigned:
			counts["assigned"]++
		case service.AttentionPRReviewRequested:
			counts["reviews"]++
		case service.AttentionMention:
			counts["mentions"]++
		}
	}

	var items []service.AttentionItem
	if kind == "all" {
		items = all
	} else {
		for _, item := range all {
			switch kind {
			case "assigned":
				if item.Kind == service.AttentionIssueAssigned || item.Kind == service.AttentionPRAssigned {
					items = append(items, item)
				}
			case "reviews":
				if item.Kind == service.AttentionPRReviewRequested {
					items = append(items, item)
				}
			case "mentions":
				if item.Kind == service.AttentionMention {
					items = append(items, item)
				}
			}
		}
	}

	switch sortBy {
	case "newest":
		sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	case "oldest":
		sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.Before(items[j].UpdatedAt) })
	default:
		sort.Slice(items, func(i, j int) bool { return items[i].WaitingSince.Before(items[j].WaitingSince) })
	}

	data := view.AttentionData{
		BasePage: withAccountSubnav(basePage(r, h.Services), "attention", h.accountCounts(ctx, claims.UserID)),
		Items:    items,
		Kind:     kind,
		Sort:     sortBy,
		Counts:   counts,
		Total:    len(all),
	}
	h.render(w, r, pages.AccountAttention(data))
}

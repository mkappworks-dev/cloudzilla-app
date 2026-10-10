package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func applyHomeRepoFilter(repos []model.Repository, filter string) []model.Repository {
	if filter == "all" || filter == "" {
		return repos
	}
	out := repos[:0:0]
	for _, r := range repos {
		switch filter {
		case "sources":
			if !r.IsFork && !r.IsTemplate {
				out = append(out, r)
			}
		case "forks":
			if r.IsFork {
				out = append(out, r)
			}
		case "templates":
			if r.IsTemplate {
				out = append(out, r)
			}
		}
	}
	return out
}

func applyHomeRepoSort(repos []model.Repository, by string) {
	switch by {
	case "name":
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].Name < repos[j].Name
		})
	case "created":
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].CreatedAt.After(repos[j].CreatedAt)
		})
	default: // "updated"
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].UpdatedAt.After(repos[j].UpdatedAt)
		})
	}
}

func (h *Handler) PageHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	templates, _ := h.Services.Repo.ListTemplates(ctx)
	if templates == nil {
		templates = []model.Repository{}
	}

	repoSort := r.URL.Query().Get("repo_sort")
	if repoSort != "name" && repoSort != "created" {
		repoSort = "updated"
	}
	repoFilter := r.URL.Query().Get("repo_filter")
	if repoFilter != "sources" && repoFilter != "forks" && repoFilter != "templates" {
		repoFilter = "all"
	}

	repos := []model.Repository{}
	data := view.HomeData{
		BasePage:   basePage(r, h.Services),
		Repos:      repos,
		RepoSort:   repoSort,
		RepoFilter: repoFilter,
		Templates:  templates,
	}

	if claims, ok := middleware.ClaimsFromContext(ctx); ok {
		userID := claims.UserID
		viewerID := userID
		ownRepos, err := h.Services.Repo.ListByOwnerVisibleTo(ctx, claims.Username, &viewerID)
		if err != nil {
			slog.Error("home: own-repo list failed", "user_id", userID, "error", err)
			data.LoadWarnings = append(data.LoadWarnings,
				"We couldn't load your repositories right now. Refresh to try again.")
		} else {
			data.Repos = applyHomeRepoFilter(ownRepos, repoFilter)
			applyHomeRepoSort(data.Repos, repoSort)
		}
	} else {
		publicRepos, err := h.Services.Repo.ListVisibleTo(ctx, nil)
		if err != nil {
			slog.Warn("home: anonymous public-repo list failed", "error", err)
		} else {
			data.Repos = applyHomeRepoFilter(publicRepos, repoFilter)
			applyHomeRepoSort(data.Repos, repoSort)
		}
	}

	if len(data.Repos) > 0 {
		repoIDs := make([]int64, len(data.Repos))
		for i, repo := range data.Repos {
			repoIDs[i] = repo.ID
		}
		if prCounts, err := h.Services.Pull.CountOpenByRepoIDs(ctx, repoIDs); err != nil {
			slog.Warn("home: open PR counts failed", "error", err)
			data.RepoOpenPRs = map[int64]int{}
		} else {
			data.RepoOpenPRs = prCounts
		}
	}
	if claims, ok := middleware.ClaimsFromContext(ctx); ok {
		userID := claims.UserID
		statWarned := false
		statFailed := func(stat string, err error) {
			slog.Warn("home: "+stat+" failed", "user_id", userID, "error", err)
			if !statWarned {
				data.LoadWarnings = append(data.LoadWarnings,
					"Some of your dashboard couldn't be loaded right now. Refresh to try again.")
				statWarned = true
			}
		}
		commitsLast7, err := h.Services.CommitStats.CommitsForUserSince(ctx, userID, 7)
		commitsFailed := err != nil
		if err != nil {
			statFailed("commits-last-7 stat", err)
		}
		distinctRepos, err := h.Services.CommitStats.DistinctReposForUserSince(ctx, userID, 7)
		if err != nil {
			statFailed("distinct-repos-7 stat", err)
		}
		countRepos, err := h.Services.Repo.CountForUser(ctx, userID)
		reposFailed := err != nil
		if err != nil {
			statFailed("repo count stat", err)
		}
		countOpenPulls, err := h.Services.Pull.CountOpenAssignedTo(ctx, userID)
		pullsFailed := err != nil
		if err != nil {
			statFailed("open-pulls count stat", err)
		}
		countAwaitingReview, err := h.Services.Pull.CountAwaitingReview(ctx, userID)
		if err != nil {
			statFailed("awaiting-review count stat", err)
		}
		countOpenIssues, err := h.Services.Issue.CountOpenAssignedTo(ctx, userID)
		issuesFailed := err != nil
		if err != nil {
			statFailed("open-issues count stat", err)
		}
		countDueThisWeek, err := h.Services.Issue.CountDueThisWeekAssignedTo(ctx, userID)
		if err != nil {
			statFailed("issues-due-this-week count stat", err)
		}
		privateRepos := 0
		for _, repo := range data.Repos {
			if repo.Private {
				privateRepos++
			}
		}
		reposDetail := ""
		if len(data.Repos) > 0 {
			reposDetail = fmt.Sprintf("%d private, %d public", privateRepos, len(data.Repos)-privateRepos)
		}
		pullsDetail := ""
		if countAwaitingReview == 1 {
			pullsDetail = "1 awaiting your review"
		} else if countAwaitingReview > 1 {
			pullsDetail = fmt.Sprintf("%d awaiting your review", countAwaitingReview)
		}
		commitsDetail := ""
		if distinctRepos == 1 {
			commitsDetail = "across 1 repository"
		} else if distinctRepos > 1 {
			commitsDetail = fmt.Sprintf("across %d repositories", distinctRepos)
		}
		issuesDetail := ""
		if countDueThisWeek == 1 {
			issuesDetail = "1 due this week"
		} else if countDueThisWeek > 1 {
			issuesDetail = fmt.Sprintf("%d due this week", countDueThisWeek)
		}
		data.TotalRepos = countRepos
		data.Stats = []components.StatItem{
			{Label: "Repositories", Value: countRepos, Detail: reposDetail, Unavailable: reposFailed},
			{Label: "Pull requests", Value: countOpenPulls, Subtitle: "open", Detail: pullsDetail, Unavailable: pullsFailed},
			{Label: "Issues", Value: countOpenIssues, Subtitle: "assigned", Detail: issuesDetail, Unavailable: issuesFailed},
			{Label: "Commits, last 7 days", Value: commitsLast7, Detail: commitsDetail, Unavailable: commitsFailed},
		}
		data.BasePage = withAccountSubnav(data.BasePage, "overview", h.accountCounts(ctx, userID))
		heatmapYear := time.Now().UTC().Year()
		if y, perr := strconv.Atoi(r.URL.Query().Get("year")); perr == nil && y >= 2000 && y <= heatmapYear {
			heatmapYear = y
		}
		data.HeatmapYear = heatmapYear
		if heat, total, err := h.Services.CommitStats.HeatmapForYear(ctx, userID, heatmapYear); err != nil {
			statFailed("heatmap", err)
		} else {
			data.Heatmap = heat
			data.HeatmapTotal = total
		}
		yearSet := map[int]bool{heatmapYear: true, time.Now().UTC().Year(): true}
		if ys, err := h.Services.CommitStats.CommitYearsForUser(ctx, userID); err != nil {
			statFailed("commit years", err)
		} else {
			for _, y := range ys {
				yearSet[y] = true
			}
		}
		years := make([]int, 0, len(yearSet))
		for y := range yearSet {
			years = append(years, y)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(years)))
		data.HeatmapYears = years
		if att, err := h.Services.Attention.ForUser(ctx, userID); err != nil {
			statFailed("attention list", err)
		} else {
			data.AttentionTotal = len(att)
			if len(att) > 3 {
				data.Attention = att[:3]
			} else {
				data.Attention = att
			}
		}
		if feed, err := h.Services.Event.Feed(ctx, int(userID), "all", 1, 10); err != nil {
			statFailed("activity feed", err)
		} else {
			data.Activity = feed
		}
	}

	h.render(w, r, pages.Home(data))
}

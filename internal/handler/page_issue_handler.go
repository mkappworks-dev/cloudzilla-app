package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageIssues renders the paginated issue list for a repository.
func (h *Handler) PageIssues(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !repo.AllowIssues {
		h.NotFound(w, r)
		return
	}

	var callerID *int64
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	stateFilter := r.URL.Query().Get("state")
	if stateFilter != "closed" {
		stateFilter = "open"
	}
	searchQuery := r.URL.Query().Get("q")
	labelFilter := r.URL.Query().Get("label")
	milestoneFilter := r.URL.Query().Get("milestone")
	sortOrder := r.URL.Query().Get("sort")
	if sortOrder == "" {
		sortOrder = "newest"
	}

	allIssues, err := h.Services.Issue.List(r.Context(), owner, repoName, callerID)
	if err != nil {
		slog.Error("issues: list failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "failed to load issues", http.StatusInternalServerError)
		return
	}
	if allIssues == nil {
		allIssues = []model.Issue{}
	}

	openCount := 0
	closedCount := 0
	for _, iss := range allIssues {
		if string(iss.State) == "open" {
			openCount++
		} else {
			closedCount++
		}
	}

	var issues []model.Issue
	for _, iss := range allIssues {
		if string(iss.State) == stateFilter {
			issues = append(issues, iss)
		}
	}
	if issues == nil {
		issues = []model.Issue{}
	}

	issueLabels, err := h.Services.Label.BatchForIssues(r.Context(), issues)
	if err != nil {
		slog.Warn("issues: label batch failed", "owner", owner, "repo", repoName, "error", err)
	}
	if issueLabels == nil {
		issueLabels = map[int64][]model.Label{}
	}

	if searchQuery != "" {
		q := strings.ToLower(searchQuery)
		filtered := issues[:0]
		for _, iss := range issues {
			if strings.Contains(strings.ToLower(iss.Title), q) {
				filtered = append(filtered, iss)
			}
		}
		issues = filtered
	}

	if labelFilter != "" {
		filtered := issues[:0]
		for _, iss := range issues {
			for _, l := range issueLabels[iss.ID] {
				if l.Name == labelFilter {
					filtered = append(filtered, iss)
					break
				}
			}
		}
		issues = filtered
	}

	if milestoneFilter != "" {
		if milestoneID, convErr := strconv.ParseInt(milestoneFilter, 10, 64); convErr == nil {
			filtered := issues[:0]
			for _, iss := range issues {
				if iss.MilestoneID != nil && *iss.MilestoneID == milestoneID {
					filtered = append(filtered, iss)
				}
			}
			issues = filtered
		}
	}

	switch sortOrder {
	case "oldest":
		sort.SliceStable(issues, func(i, j int) bool {
			return issues[i].CreatedAt.Before(issues[j].CreatedAt)
		})
	case "recently-updated":
		sort.SliceStable(issues, func(i, j int) bool {
			return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
		})
	default:
		sort.SliceStable(issues, func(i, j int) bool {
			return issues[i].CreatedAt.After(issues[j].CreatedAt)
		})
	}

	// Second pass: the label map must cover only the post-filter slice.
	issueLabels, err = h.Services.Label.BatchForIssues(r.Context(), issues)
	if err != nil {
		slog.Warn("issues: label batch failed", "owner", owner, "repo", repoName, "error", err)
	}
	if issueLabels == nil {
		issueLabels = map[int64][]model.Label{}
	}

	allMilestones, err := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("issues: milestone list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if allMilestones == nil {
		allMilestones = []model.Milestone{}
	}

	allLabels, err := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("issues: label list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if allLabels == nil {
		allLabels = []model.Label{}
	}

	pinnedIssues, err := h.Services.Issue.ListPinned(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("issues: pinned list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if pinnedIssues == nil {
		pinnedIssues = []model.Issue{}
	}

	h.render(w, r, pages.Issues(view.IssuesData{
		BasePage:        h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "issues", canManage),
		Repo:            *repo,
		Issues:          issues,
		PinnedIssues:    pinnedIssues,
		Owner:           owner,
		RepoName:        repoName,
		IssueLabels:     issueLabels,
		AllMilestones:   allMilestones,
		StateFilter:     stateFilter,
		SearchQuery:     searchQuery,
		LabelFilter:     labelFilter,
		MilestoneFilter: milestoneFilter,
		Sort:            sortOrder,
		Labels:          allLabels,
		OpenCount:       openCount,
		ClosedCount:     closedCount,
	}))
}

// PageIssueDetail renders the issue detail page with comments and sidebar.
func (h *Handler) PageIssueDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		http.Error(w, "invalid issue number", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !repo.AllowIssues {
		h.NotFound(w, r)
		return
	}

	var issueCallerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		issueCallerID = &claims.UserID
	}
	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, number, issueCallerID)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	rawComments, _ := h.Services.Comment.ListByIssue(r.Context(), issue.ID)
	if rawComments == nil {
		rawComments = []model.Comment{}
	}
	rendered := make([]RenderedComment, len(rawComments))
	for i, c := range rawComments {
		rendered[i] = RenderedComment{Comment: c, BodyHTML: renderMentionsHTML(markdown.Render(c.Body))}
	}

	issueLabels, _ := h.Services.Label.GetForIssue(r.Context(), issue.ID)
	if issueLabels == nil {
		issueLabels = []model.Label{}
	}
	allLabels, _ := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if allLabels == nil {
		allLabels = []model.Label{}
	}
	issueAssignees, _ := h.Services.Assignee.GetForIssue(r.Context(), issue.ID)
	if issueAssignees == nil {
		issueAssignees = []model.User{}
	}

	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	issueMilestone, _ := h.Services.Milestone.GetForIssue(r.Context(), issue.ID)
	allIssueMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if allIssueMilestones == nil {
		allIssueMilestones = []model.Milestone{}
	}

	linkedPRs, err := h.Services.Issue.LinkedPRs(r.Context(), owner, repoName, issue.Number)
	if err != nil {
		slog.Warn("issue detail: linked PRs lookup failed", "owner", owner, "repo", repoName, "issue", issue.Number, "error", err)
	}
	if linkedPRs == nil {
		linkedPRs = []model.PullRequest{}
	}

	repoPulls, err := h.Services.Pull.List(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("issue detail: repo PR list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if repoPulls == nil {
		repoPulls = []model.PullRequest{}
	}

	collaborators, err := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	if err != nil {
		slog.Warn("issue detail: list collaborators failed", "owner", owner, "repo", repoName, "error", err)
	}

	h.render(w, r, pages.IssueDetail(view.IssueDetailData{
		BasePage:      h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "issues", canManage),
		Repo:          *repo,
		Issue:         *issue,
		Comments:      rendered,
		Owner:         owner,
		RepoName:      repoName,
		BodyHTML:      markdown.Render(issue.Body),
		Labels:        issueLabels,
		Assignees:     issueAssignees,
		AllLabels:     allLabels,
		Milestone:     issueMilestone,
		AllMilestones: allIssueMilestones,
		LinkedPRs:     linkedPRs,
		RepoPulls:     repoPulls,
		Collaborators: collaboratorUsernames(collaborators),
		CanWrite:      canWrite,
		CanManage:     canManage,
	}))
}

// PageNewIssue renders the new issue form, optionally pre-filled from an issue template.
func (h *Handler) PageNewIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !repo.AllowIssues {
		h.NotFound(w, r)
		return
	}

	templates, _ := h.Services.Code.GetIssueTemplates(owner, repoName, repo.DefaultBranch)

	slug := r.URL.Query().Get("template")
	blank := r.URL.Query().Get("blank") == "1"
	selected := ""
	for _, t := range templates {
		if t.Slug == slug {
			selected = t.Body
			break
		}
	}
	showForm := blank || selected != "" || len(templates) == 0

	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}
	data := view.IssueNewData{
		BasePage:  h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "issues", canManage),
		Repo:      *repo,
		Owner:     owner,
		RepoName:  repoName,
		Templates: templates,
		Selected:  selected,
		ShowForm:  showForm,
		CanWrite:  canWrite,
	}
	if canWrite {
		h.loadIssueSidebarOptions(r, &data, repo, owner, repoName)
	}
	h.render(w, r, pages.IssueNew(data))
}

// loadIssueSidebarOptions populates the new-issue metadata picker options
// (collaborators, labels, milestones). Best-effort: a failed query leaves that
// picker empty rather than failing the page.
func (h *Handler) loadIssueSidebarOptions(r *http.Request, data *view.IssueNewData, repo *model.Repository, owner, repoName string) {
	ctx := r.Context()
	if collabs, err := h.Services.Repo.ListCollaborators(ctx, repo.ID); err == nil {
		data.Collaborators = collaboratorUsernames(collabs)
	} else {
		slog.Warn("new issue: list collaborators failed", "owner", owner, "repo", repoName, "error", err)
	}
	if labels, err := h.Services.Label.ListByRepo(ctx, owner, repoName); err == nil {
		data.Labels = labels
	} else {
		slog.Warn("new issue: list labels failed", "owner", owner, "repo", repoName, "error", err)
	}
	if milestones, err := h.Services.Milestone.ListByRepo(ctx, owner, repoName); err == nil {
		data.Milestones = milestones
	} else {
		slog.Warn("new issue: list milestones failed", "owner", owner, "repo", repoName, "error", err)
	}
	if pulls, err := h.Services.Pull.List(ctx, owner, repoName); err == nil {
		data.RepoPulls = pullsToLinkedPulls(pulls)
	} else {
		slog.Warn("new issue: list pulls failed", "owner", owner, "repo", repoName, "error", err)
	}
}

// applyNewIssueMetadata applies the assignees, labels, priority, and milestone
// chosen on the new-issue form. Best-effort: one field failing is logged and
// skipped rather than failing the already-created issue. Returns the kinds of
// metadata that failed so the caller can surface a single consolidated signal
// (one log line per submit + an optional ?warn=metadata redirect param) rather
// than scattered per-field warnings the user never sees.
func (h *Handler) applyNewIssueMetadata(r *http.Request, owner, repoName string, issue *model.Issue) []string {
	ctx := r.Context()
	var failed []string
	for _, username := range r.Form["assignees"] {
		if username == "" {
			continue
		}
		if err := h.Services.Assignee.AddToIssue(ctx, owner, repoName, issue.Number, username); err != nil {
			slog.Warn("new issue: add assignee failed", "issue", issue.Number, "user", username, "error", err)
			failed = append(failed, "assignee")
		}
	}
	for _, raw := range r.Form["labels"] {
		labelID, convErr := strconv.ParseInt(raw, 10, 64)
		if convErr != nil {
			continue
		}
		if err := h.Services.Label.AddToIssue(ctx, owner, repoName, issue.Number, labelID); err != nil {
			slog.Warn("new issue: add label failed", "issue", issue.Number, "label", labelID, "error", err)
			failed = append(failed, "label")
		}
	}
	switch p := r.FormValue("priority"); p {
	case "P0", "P1", "P2", "P3":
		if _, err := h.Services.Issue.SetPriority(ctx, owner, repoName, issue.Number, &p); err != nil {
			slog.Warn("new issue: set priority failed", "issue", issue.Number, "priority", p, "error", err)
			failed = append(failed, "priority")
		}
	}
	if m := r.FormValue("milestone"); m != "" {
		if milestoneID, convErr := strconv.ParseInt(m, 10, 64); convErr == nil {
			if err := h.Services.Milestone.SetIssue(ctx, issue.ID, &milestoneID); err != nil {
				slog.Warn("new issue: set milestone failed", "issue", issue.Number, "milestone", milestoneID, "error", err)
				failed = append(failed, "milestone")
			}
		}
	}
	for _, raw := range r.Form["linked_pulls"] {
		pullNumber, convErr := strconv.Atoi(raw)
		if convErr != nil {
			continue
		}
		pull, perr := h.Services.Pull.Get(ctx, owner, repoName, pullNumber)
		if perr != nil {
			slog.Warn("new issue: link pull lookup failed", "issue", issue.Number, "pull", pullNumber, "error", perr)
			failed = append(failed, "linked_pull")
			continue
		}
		if err := h.Services.Issue.LinkPull(ctx, pull.ID, issue.ID); err != nil {
			slog.Warn("new issue: link pull failed", "issue", issue.Number, "pull", pullNumber, "error", err)
			failed = append(failed, "linked_pull")
		}
	}
	if len(failed) > 0 {
		slog.Warn("new issue: some metadata failed to apply",
			"issue", issue.Number, "owner", owner, "repo", repoName, "failed", failed)
	}
	return failed
}

// PageNewIssueSubmit handles new issue form submission and redirects to the created issue.
func (h *Handler) PageNewIssueSubmit(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	title := r.FormValue("title")
	body := r.FormValue("body")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !repo.AllowIssues {
		h.NotFound(w, r)
		return
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	templates, _ := h.Services.Code.GetIssueTemplates(owner, repoName, repo.DefaultBranch)
	renderErr := func(msg string) {
		data := view.IssueNewData{
			BasePage:  h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "issues", canManage),
			Repo:      *repo,
			Owner:     owner,
			RepoName:  repoName,
			Templates: templates,
			Selected:  body,
			ShowForm:  true,
			CanWrite:  canWrite,
			Error:     msg,
		}
		if canWrite {
			h.loadIssueSidebarOptions(r, &data, repo, owner, repoName)
		}
		h.render(w, r, pages.IssueNew(data))
	}

	if title == "" {
		renderErr("Title is required")
		return
	}

	vis := r.FormValue("visibility")
	if vis != "private" {
		vis = "public"
	}
	issue, err := h.Services.Issue.Create(r.Context(), owner, repoName, claims.UserID, title, body, vis)
	if err != nil {
		renderErr("Failed to create issue: " + err.Error())
		return
	}

	var metadataFailures []string
	if canWrite {
		metadataFailures = h.applyNewIssueMetadata(r, owner, repoName, issue)
	}

	go h.Services.Webhook.Dispatch(repo.ID, "issues", h.Services.Webhook.IssuePayload("opened", *repo, *issue))

	redirectURL := fmt.Sprintf("/%s/%s/issues/%d", owner, repoName, issue.Number)
	if len(metadataFailures) > 0 {
		redirectURL += "?warn=metadata"
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

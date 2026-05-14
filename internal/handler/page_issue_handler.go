package handler

import (
	"fmt"
	"net/http"
	"strconv"

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

	var callerID *int64
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}
	issues, err := h.Services.Issue.List(r.Context(), owner, repoName, callerID)
	if err != nil {
		issues = []model.Issue{}
	}
	if issues == nil {
		issues = []model.Issue{}
	}

	issueLabels, _ := h.Services.Label.BatchForIssues(r.Context(), issues)
	if issueLabels == nil {
		issueLabels = map[int64][]model.Label{}
	}

	allMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if allMilestones == nil {
		allMilestones = []model.Milestone{}
	}

	pinnedIssues, _ := h.Services.Issue.ListPinned(r.Context(), owner, repoName)
	if pinnedIssues == nil {
		pinnedIssues = []model.Issue{}
	}

	h.render(w, r, pages.Issues(view.IssuesData{
		BasePage:      withRepoSubnav(basePage(r, h.Services), owner, repoName, "issues", canManage),
		Repo:          *repo,
		Issues:        issues,
		PinnedIssues:  pinnedIssues,
		Owner:         owner,
		RepoName:      repoName,
		IssueLabels:   issueLabels,
		AllMilestones: allMilestones,
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

	h.render(w, r, pages.IssueDetail(view.IssueDetailData{
		BasePage:      withRepoSubnav(basePage(r, h.Services), owner, repoName, "issues", canManage),
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
	h.render(w, r, pages.IssueNew(view.IssueNewData{
		BasePage:  withRepoSubnav(basePage(r, h.Services), owner, repoName, "issues", canManage),
		Repo:      *repo,
		Owner:     owner,
		RepoName:  repoName,
		Templates: templates,
		Selected:  selected,
		ShowForm:  showForm,
		CanWrite:  canWrite,
	}))
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

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	renderErr := func(msg string) {
		h.render(w, r, pages.IssueNew(view.IssueNewData{
			BasePage: withRepoSubnav(basePage(r, h.Services), owner, repoName, "issues", canManage),
			Repo:     *repo,
			Owner:    owner,
			RepoName: repoName,
			Selected: body,
			ShowForm: true,
			Error:    msg,
		}))
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

	go h.Services.Webhook.Dispatch(repo.ID, "issues", h.Services.Webhook.IssuePayload("opened", *repo, *issue))

	http.Redirect(w, r, fmt.Sprintf("/%s/%s/issues/%d", owner, repoName, issue.Number), http.StatusSeeOther)
}

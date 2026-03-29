package handler

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func basePage(r *http.Request, services *service.Services) BasePage {
	allowLogin := services.SiteSetting.AllowLogin(r.Context())
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return BasePage{AllowLogin: allowLogin}
	}
	count, _ := services.Notification.CountUnread(r.Context(), claims.UserID)
	return BasePage{CurrentUser: &claims, UnreadNotifCount: count, AllowLogin: allowLogin}
}

func (h *Handler) PageHome(w http.ResponseWriter, r *http.Request) {
	repos, err := h.Services.Repo.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list repos", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []model.Repository{}
	}
	h.render(w, r, pages.Home(view.HomeData{BasePage: basePage(r, h.Services), Repos: repos}))
}

func (h *Handler) PageLogin(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.Login(view.LoginData{BasePage: basePage(r, h.Services)}))
}

func (h *Handler) PageLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		h.render(w, r, pages.Login(view.LoginData{BasePage: basePage(r, h.Services), Error: "Invalid credentials"}))
		return
	}

	if !user.IsSuperadmin && !user.IsInvited && !h.Services.SiteSetting.AllowLogin(r.Context()) {
		h.render(w, r, pages.Login(view.LoginData{BasePage: basePage(r, h.Services), Error: "Login is currently disabled"}))
		return
	}

	totpEnabled, _, err := h.Services.TOTP.GetUserTOTPState(r.Context(), user.ID)
	if err != nil {
		h.render(w, r, pages.Login(view.LoginData{BasePage: basePage(r, h.Services), Error: "Internal error"}))
		return
	}

	if totpEnabled {
		pendingToken, err := h.Services.TOTP.GeneratePendingToken(user.ID, h.Cfg.Auth.JWTSecret)
		if err != nil {
			h.render(w, r, pages.Login(view.LoginData{BasePage: basePage(r, h.Services), Error: "Internal error"}))
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     totpPendingCookieName,
			Value:    pendingToken,
			HttpOnly: true,
			Path:     "/",
			Expires:  time.Now().Add(5 * time.Minute),
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/auth/2fa", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

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
		repos = []model.Repository{}
	}
	if repos == nil {
		repos = []model.Repository{}
	}

	h.render(w, r, pages.User(view.UserData{
		BasePage: basePage(r, h.Services),
		User:     *user,
		Repos:    repos,
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

func (h *Handler) PageRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if hostWithoutPort, _, err := net.SplitHostPort(host); err == nil {
		host = hostWithoutPort
	}

	cloneHTTP := fmt.Sprintf("%s://%s/%s/%s.git", scheme, r.Host, owner, repoName)
	cloneSSH := fmt.Sprintf("ssh://git@%s:%d/%s/%s.git", host, h.Cfg.Git.SSHPort, owner, repoName)

	canWrite := false
	var currentUserID int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		currentUserID = claims.UserID
	}

	var readmeHTML string
	for _, name := range []string{"README.md", "readme.md", "Readme.md"} {
		raw, err := h.Services.Code.GetRawBlob(owner, repoName, repo.DefaultBranch, name)
		if err == nil {
			readmeHTML = markdown.Render(string(raw))
			break
		}
	}

	starCount, _ := h.Services.Star.GetStarCount(r.Context(), repo.ID)
	isStarred := false
	if currentUserID != 0 {
		isStarred, _ = h.Services.Star.IsStarred(r.Context(), repo.ID, currentUserID)
	}

	forkOfPath := ""
	if repo.IsFork && repo.ForkOfOwner != "" {
		forkOfPath = repo.ForkOfOwner + "/" + repo.ForkOfName
	}

	latestRelease, _ := h.Services.Release.GetLatest(r.Context(), owner, repoName)

	h.render(w, r, pages.Repo(view.RepoData{
		BasePage:      basePage(r, h.Services),
		Repo:          *repo,
		Owner:         owner,
		RepoName:      repoName,
		CloneHTTP:     cloneHTTP,
		CloneSSH:      cloneSSH,
		CanWrite:      canWrite,
		ReadmeHTML:    readmeHTML,
		StarCount:     starCount,
		IsStarred:     isStarred,
		ForkCount:     repo.ForkCount,
		IsFork:        repo.IsFork,
		ForkOfPath:    forkOfPath,
		LatestRelease: latestRelease,
	}))
}

func (h *Handler) PageRepoSettings(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	webhooks, _ := h.Services.Webhook.ListByRepo(r.Context(), repo.ID)
	if webhooks == nil {
		webhooks = []model.Webhook{}
	}

	collabs, _ := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	if collabs == nil {
		collabs = []model.Permission{}
	}

	labels, _ := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if labels == nil {
		labels = []model.Label{}
	}

	deployKeys, _ := h.Services.DeployKey.List(r.Context(), repo.ID)
	if deployKeys == nil {
		deployKeys = []model.DeployKey{}
	}

	branchProtections, _ := h.Services.BranchProtection.List(r.Context(), repo.ID)
	if branchProtections == nil {
		branchProtections = []*model.BranchProtection{}
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	// Transfer is only for personal repo owners (not org repos)
	canTransfer := repo.OwnerID == claims.UserID && repo.OrgID == 0

	h.render(w, r, pages.RepoSettings(view.RepoSettingsData{
		BasePage:          basePage(r, h.Services),
		Repo:              *repo,
		Owner:             owner,
		RepoName:          repoName,
		Webhooks:          webhooks,
		Collabs:           collabs,
		Labels:            labels,
		DeployKeys:        deployKeys,
		BranchProtections: branchProtections,
		CanManage:         canManage,
		CanTransfer:       canTransfer,
	}))
}

func (h *Handler) PageIssues(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	issues, err := h.Services.Issue.List(r.Context(), owner, repoName)
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

	h.render(w, r, pages.Issues(view.IssuesData{
		BasePage:      basePage(r, h.Services),
		Repo:          *repo,
		Issues:        issues,
		Owner:         owner,
		RepoName:      repoName,
		IssueLabels:   issueLabels,
		AllMilestones: allMilestones,
	}))
}

func (h *Handler) PageIssueDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	issue, err := h.Services.Issue.Get(r.Context(), owner, repoName, number)
	if err != nil {
		http.Error(w, "issue not found", http.StatusNotFound)
		return
	}

	rawComments, _ := h.Services.Comment.ListByIssue(r.Context(), issue.ID)
	if rawComments == nil {
		rawComments = []model.Comment{}
	}
	rendered := make([]RenderedComment, len(rawComments))
	for i, c := range rawComments {
		rendered[i] = RenderedComment{Comment: c, BodyHTML: markdown.Render(c.Body)}
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
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	issueMilestone, _ := h.Services.Milestone.GetForIssue(r.Context(), issue.ID)
	allIssueMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if allIssueMilestones == nil {
		allIssueMilestones = []model.Milestone{}
	}

	h.render(w, r, pages.IssueDetail(view.IssueDetailData{
		BasePage:      basePage(r, h.Services),
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
	}))
}

func (h *Handler) PageNewIssue(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
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

	h.render(w, r, pages.IssueNew(view.IssueNewData{
		BasePage:  basePage(r, h.Services),
		Repo:      *repo,
		Owner:     owner,
		RepoName:  repoName,
		Templates: templates,
		Selected:  selected,
		ShowForm:  showForm,
	}))
}

func (h *Handler) PageNewIssueSubmit(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	r.ParseForm()
	title := r.FormValue("title")
	body := r.FormValue("body")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	renderErr := func(msg string) {
		h.render(w, r, pages.IssueNew(view.IssueNewData{
			BasePage: basePage(r, h.Services),
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

	issue, err := h.Services.Issue.Create(r.Context(), owner, repoName, claims.UserID, title, body)
	if err != nil {
		renderErr("Failed to create issue: " + err.Error())
		return
	}

	go h.Services.Webhook.Dispatch(repo.ID, "issues", h.Services.Webhook.IssuePayload("opened", *repo, *issue))

	http.Redirect(w, r, fmt.Sprintf("/%s/%s/issues/%d", owner, repoName, issue.Number), http.StatusSeeOther)
}

func (h *Handler) PagePulls(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	allPulls, err := h.Services.Pull.List(r.Context(), owner, repoName)
	if err != nil {
		allPulls = []model.PullRequest{}
	}
	if allPulls == nil {
		allPulls = []model.PullRequest{}
	}

	stateFilter := r.URL.Query().Get("state")
	if stateFilter == "" {
		stateFilter = "open"
	}
	var pulls []model.PullRequest
	for _, p := range allPulls {
		switch stateFilter {
		case "draft":
			if p.IsDraft && p.State == model.PRStateOpen {
				pulls = append(pulls, p)
			}
		case "closed":
			if p.State == model.PRStateClosed {
				pulls = append(pulls, p)
			}
		case "merged":
			if p.State == model.PRStateMerged {
				pulls = append(pulls, p)
			}
		default: // "open"
			if p.State == model.PRStateOpen && !p.IsDraft {
				pulls = append(pulls, p)
			}
		}
	}
	if pulls == nil {
		pulls = []model.PullRequest{}
	}

	pullLabels, _ := h.Services.Label.BatchForPulls(r.Context(), pulls)
	if pullLabels == nil {
		pullLabels = map[int64][]model.Label{}
	}

	allPullMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if allPullMilestones == nil {
		allPullMilestones = []model.Milestone{}
	}

	h.render(w, r, pages.Pulls(view.PullsData{
		BasePage:      basePage(r, h.Services),
		Repo:          *repo,
		Pulls:         pulls,
		Owner:         owner,
		RepoName:      repoName,
		PullLabels:    pullLabels,
		AllMilestones: allPullMilestones,
		StateFilter:   stateFilter,
	}))
}

func (h *Handler) PageNewPull(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	templateBody, _ := h.Services.Code.GetPRTemplate(owner, repoName, repo.DefaultBranch)
	refs, _ := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	var branches []service.BranchInfo
	if refs != nil {
		branches = refs.Branches
	}

	h.render(w, r, pages.PullNew(view.PullNewData{
		BasePage:     basePage(r, h.Services),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		TemplateBody: templateBody,
		Branches:     branches,
	}))
}

func (h *Handler) PageNewPullSubmit(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	refs, _ := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	var branches []service.BranchInfo
	if refs != nil {
		branches = refs.Branches
	}

	r.ParseForm()
	title := r.FormValue("title")
	body := r.FormValue("body")
	headBranch := r.FormValue("head_branch")
	baseBranch := r.FormValue("base_branch")

	renderErr := func(msg string) {
		h.render(w, r, pages.PullNew(view.PullNewData{
			BasePage:     basePage(r, h.Services),
			Repo:         *repo,
			Owner:        owner,
			RepoName:     repoName,
			TemplateBody: body,
			Branches:     branches,
			Error:        msg,
		}))
	}

	if title == "" {
		renderErr("Title is required")
		return
	}

	pr, err := h.Services.Pull.Create(r.Context(), owner, repoName, claims.UserID, title, body, headBranch, baseBranch, false)
	if err != nil {
		renderErr("Failed to create pull request: " + err.Error())
		return
	}

	go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload("opened", *repo, *pr))

	http.Redirect(w, r, fmt.Sprintf("/%s/%s/pulls/%d", owner, repoName, pr.Number), http.StatusSeeOther)
}

func (h *Handler) PagePullDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		http.Error(w, "pull request not found", http.StatusNotFound)
		return
	}

	var diff *service.PRDiffResult
	if pull.State == model.PRStateOpen {
		if d, err := h.Services.Code.GetPullDiff(owner, repoName, pull.BaseBranch, pull.HeadBranch); err == nil {
			diff = d
		}
	}

	pullLabels2, _ := h.Services.Label.GetForPull(r.Context(), pull.ID)
	if pullLabels2 == nil {
		pullLabels2 = []model.Label{}
	}
	allLabels2, _ := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if allLabels2 == nil {
		allLabels2 = []model.Label{}
	}
	pullAssignees, _ := h.Services.Assignee.GetForPull(r.Context(), pull.ID)
	if pullAssignees == nil {
		pullAssignees = []model.User{}
	}

	canWrite2 := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite2 = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	var headStatuses []model.CommitStatus
	if headCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pull.HeadBranch); err == nil {
		headStatuses, _ = h.Services.CommitStatus.List(r.Context(), owner, repoName, headCommit.Hash.String())
	}
	if headStatuses == nil {
		headStatuses = []model.CommitStatus{}
	}

	pullMilestone, _ := h.Services.Milestone.GetForPull(r.Context(), pull.ID)
	allPullDetailMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if allPullDetailMilestones == nil {
		allPullDetailMilestones = []model.Milestone{}
	}

	reviews, _ := h.Services.PullReview.ListByPull(r.Context(), owner, repoName, number)
	if reviews == nil {
		reviews = []model.PullReview{}
	}
	canMerge, mergeBlockReason, _ := h.Services.PullReview.CanMerge(r.Context(), pull.ID)

	rawLineComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
	lineComments := map[string][]RenderedLineComment{}
	for _, c := range rawLineComments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		lineComments[key] = append(lineComments[key], RenderedLineComment{
			PullLineComment: c,
			BodyHTML:        markdown.Render(c.Body),
		})
	}

	h.render(w, r, pages.PullDetail(view.PullDetailData{
		BasePage:          basePage(r, h.Services),
		Repo:              *repo,
		Pull:              *pull,
		Owner:             owner,
		RepoName:          repoName,
		Diff:              diff,
		BodyHTML:          markdown.Render(pull.Body),
		Labels:            pullLabels2,
		Assignees:         pullAssignees,
		AllLabels:         allLabels2,
		Milestone:         pullMilestone,
		AllMilestones:     allPullDetailMilestones,
		CanWrite:          canWrite2,
		HeadStatuses:      headStatuses,
		Reviews:           reviews,
		CanMerge:          canMerge,
		MergeBlockReason:  mergeBlockReason,
		AutoMergeEnabled:  pull.AutoMergeEnabled,
		AutoMergeStrategy: pull.AutoMergeStrategy,
		LineComments:      lineComments,
	}))
}

func (h *Handler) PageSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	keys, err := h.Services.SSHKey.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		keys = []model.SSHKey{}
	}
	if keys == nil {
		keys = []model.SSHKey{}
	}

	h.render(w, r, pages.Settings(view.SettingsData{
		BasePage: basePage(r, h.Services),
		SSHKeys:  keys,
	}))
}

func (h *Handler) PageRefs(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	result, err := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	canWrite := userID != nil && h.Services.Repo.CanWrite(r.Context(), repo, *userID)

	h.render(w, r, pages.Refs(view.RefsData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Branches: result.Branches,
		Tags:     result.Tags,
		CanWrite: canWrite,
	}))
}

func (h *Handler) PageTree(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "path")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	result, err := h.Services.Code.GetTree(owner, repoName, ref, path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	h.render(w, r, pages.Tree(view.TreeData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Ref:         result.Ref,
		Path:        result.Path,
		Breadcrumbs: result.Breadcrumbs,
		Entries:     result.Entries,
		RefsURL:     "/" + owner + "/" + repoName + "/refs",
	}))
}

func (h *Handler) PageBlob(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "path")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	result, err := h.Services.Code.GetBlob(owner, repoName, ref, path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	h.render(w, r, pages.Blob(view.BlobData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Ref:         result.Ref,
		Path:        result.Path,
		Breadcrumbs: result.Breadcrumbs,
		Lines:       result.Lines,
		IsBinary:    result.IsBinary,
		BlameURL:    result.BlameURL,
	}))
}

func (h *Handler) PageCommits(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	log, err := h.Services.Code.GetCommits(owner, repoName, ref, page, 30)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	h.render(w, r, pages.Commits(view.CommitsData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Log:      log,
		RefsURL:  "/" + owner + "/" + repoName + "/refs",
	}))
}

func (h *Handler) PageCommit(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	commit, err := h.Services.Code.GetCommit(owner, repoName, sha)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	statuses, _ := h.Services.CommitStatus.List(r.Context(), owner, repoName, sha)
	if statuses == nil {
		statuses = []model.CommitStatus{}
	}

	h.render(w, r, pages.Commit(view.CommitData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Commit:   commit,
		Statuses: statuses,
	}))
}

func (h *Handler) PageBlame(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "path")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	result, err := h.Services.Code.GetBlame(owner, repoName, ref, path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	h.render(w, r, pages.Blame(view.BlameData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Ref:         result.Ref,
		Path:        result.Path,
		Breadcrumbs: result.Breadcrumbs,
		Lines:       result.Lines,
		BlobURL:     result.BlobURL,
	}))
}

func (h *Handler) PageNotifications(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	notifs, _ := h.Services.Notification.List(r.Context(), claims.UserID)
	if notifs == nil {
		notifs = []model.Notification{}
	}
	unread, _ := h.Services.Notification.CountUnread(r.Context(), claims.UserID)

	h.render(w, r, pages.Notifications(view.NotificationsData{
		BasePage:      basePage(r, h.Services),
		Notifications: notifs,
		UnreadCount:   unread,
	}))
}

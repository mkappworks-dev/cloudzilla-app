package handler

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
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
	h.render(w, "home", HomeData{BasePage: basePage(r, h.Services), Repos: repos})
}

func (h *Handler) PageLogin(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login", LoginData{BasePage: basePage(r, h.Services)})
}

func (h *Handler) PageLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		h.render(w, "login", LoginData{BasePage: basePage(r, h.Services), Error: "Invalid credentials"})
		return
	}

	if !user.IsSuperadmin && !user.IsInvited && !h.Services.SiteSetting.AllowLogin(r.Context()) {
		h.render(w, "login", LoginData{BasePage: basePage(r, h.Services), Error: "Login is currently disabled"})
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

	h.render(w, "user", UserData{
		BasePage: basePage(r, h.Services),
		User:     *user,
		Repos:    repos,
	})
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

	h.render(w, "org", OrgData{
		BasePage:  basePage(r, h.Services),
		Org:       *org,
		Repos:     repos,
		Members:   members,
		CanManage: canManage,
	})
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

	h.render(w, "org_settings", OrgSettingsData{
		BasePage: basePage(r, h.Services),
		Org:      *org,
		Members:  members,
	})
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

	var readmeHTML template.HTML
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

	h.render(w, "repo", RepoData{
		BasePage:   basePage(r, h.Services),
		Repo:       *repo,
		Owner:      owner,
		RepoName:   repoName,
		CloneHTTP:  cloneHTTP,
		CloneSSH:   cloneSSH,
		CanWrite:   canWrite,
		ReadmeHTML: readmeHTML,
		StarCount:  starCount,
		IsStarred:  isStarred,
		ForkCount:  repo.ForkCount,
		IsFork:     repo.IsFork,
		ForkOfPath: forkOfPath,
	})
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

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	// Transfer is only for personal repo owners (not org repos)
	canTransfer := repo.OwnerID == claims.UserID && repo.OrgID == 0

	h.render(w, "repo_settings", RepoSettingsData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Webhooks:    webhooks,
		Collabs:     collabs,
		Labels:      labels,
		CanManage:   canManage,
		CanTransfer: canTransfer,
	})
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

	h.render(w, "issues", IssuesData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Issues:      issues,
		Owner:       owner,
		RepoName:    repoName,
		IssueLabels: issueLabels,
	})
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

	h.render(w, "issue_detail", IssueDetailData{
		BasePage:  basePage(r, h.Services),
		Repo:      *repo,
		Issue:     *issue,
		Comments:  rendered,
		Owner:     owner,
		RepoName:  repoName,
		BodyHTML:  markdown.Render(issue.Body),
		Labels:    issueLabels,
		Assignees: issueAssignees,
		AllLabels: allLabels,
		CanWrite:  canWrite,
	})
}

func (h *Handler) PagePulls(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	pulls, err := h.Services.Pull.List(r.Context(), owner, repoName)
	if err != nil {
		pulls = []model.PullRequest{}
	}
	if pulls == nil {
		pulls = []model.PullRequest{}
	}

	pullLabels, _ := h.Services.Label.BatchForPulls(r.Context(), pulls)
	if pullLabels == nil {
		pullLabels = map[int64][]model.Label{}
	}

	h.render(w, "pulls", PullsData{
		BasePage:   basePage(r, h.Services),
		Repo:       *repo,
		Pulls:      pulls,
		Owner:      owner,
		RepoName:   repoName,
		PullLabels: pullLabels,
	})
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

	h.render(w, "pull_detail", PullDetailData{
		BasePage:  basePage(r, h.Services),
		Repo:      *repo,
		Pull:      *pull,
		Owner:     owner,
		RepoName:  repoName,
		Diff:      diff,
		BodyHTML:  markdown.Render(pull.Body),
		Labels:    pullLabels2,
		Assignees: pullAssignees,
		AllLabels: allLabels2,
		CanWrite:  canWrite2,
	})
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

	h.render(w, "settings", SettingsData{
		BasePage: basePage(r, h.Services),
		SSHKeys:  keys,
	})
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

	h.render(w, "refs", RefsData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Branches: result.Branches,
		Tags:     result.Tags,
		CanWrite: canWrite,
	})
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

	h.render(w, "tree", TreeData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Ref:         result.Ref,
		Path:        result.Path,
		Breadcrumbs: result.Breadcrumbs,
		Entries:     result.Entries,
		RefsURL:     "/" + owner + "/" + repoName + "/refs",
	})
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

	h.render(w, "blob", BlobData{
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
	})
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

	h.render(w, "commits", CommitsData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Log:      log,
		RefsURL:  "/" + owner + "/" + repoName + "/refs",
	})
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

	h.render(w, "commit", CommitData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Commit:   commit,
	})
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

	h.render(w, "blame", BlameData{
		BasePage:    basePage(r, h.Services),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Ref:         result.Ref,
		Path:        result.Path,
		Breadcrumbs: result.Breadcrumbs,
		Lines:       result.Lines,
		BlobURL:     result.BlobURL,
	})
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

	h.render(w, "notifications", NotificationsData{
		BasePage:      basePage(r, h.Services),
		Notifications: notifs,
		UnreadCount:   unread,
	})
}

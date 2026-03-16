package handler

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
)

func basePage(r *http.Request) BasePage {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return BasePage{}
	}
	return BasePage{CurrentUser: &claims}
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
	h.render(w, "home", HomeData{BasePage: basePage(r), Repos: repos})
}

func (h *Handler) PageLogin(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login", LoginData{BasePage: basePage(r)})
}

func (h *Handler) PageLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	_, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		h.render(w, "login", LoginData{BasePage: basePage(r), Error: "Invalid credentials"})
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

	// Redirect to home after successful login
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) PageUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "owner")
	user, err := h.Services.User.GetByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
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
		BasePage: basePage(r),
		User:     *user,
		Repos:    repos,
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

	// Compute clone URLs
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	// Strip port from host for SSH URL
	if hostWithoutPort, _, err := net.SplitHostPort(host); err == nil {
		host = hostWithoutPort
	}

	cloneHTTP := fmt.Sprintf("%s://%s/%s/%s.git", scheme, r.Host, owner, repoName)
	cloneSSH := fmt.Sprintf("ssh://git@%s:%d/%s/%s.git", host, h.Cfg.Git.SSHPort, owner, repoName)

	h.render(w, "repo", RepoData{
		BasePage:  basePage(r),
		Repo:      *repo,
		Owner:     owner,
		RepoName:  repoName,
		CloneHTTP: cloneHTTP,
		CloneSSH:  cloneSSH,
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

	h.render(w, "issues", IssuesData{
		BasePage: basePage(r),
		Repo:     *repo,
		Issues:   issues,
		Owner:    owner,
		RepoName: repoName,
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

	comments, _ := h.Services.Comment.ListByIssue(r.Context(), issue.ID)
	if comments == nil {
		comments = []model.Comment{}
	}

	h.render(w, "issue_detail", IssueDetailData{
		BasePage: basePage(r),
		Repo:     *repo,
		Issue:    *issue,
		Comments: comments,
		Owner:    owner,
		RepoName: repoName,
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

	h.render(w, "pulls", PullsData{
		BasePage: basePage(r),
		Repo:     *repo,
		Pulls:    pulls,
		Owner:    owner,
		RepoName: repoName,
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

	h.render(w, "pull_detail", PullDetailData{
		BasePage: basePage(r),
		Repo:     *repo,
		Pull:     *pull,
		Owner:    owner,
		RepoName: repoName,
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
		BasePage: basePage(r),
		SSHKeys:  keys,
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
		BasePage:    basePage(r),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		Ref:         result.Ref,
		Path:        result.Path,
		Breadcrumbs: result.Breadcrumbs,
		Entries:     result.Entries,
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
		BasePage:    basePage(r),
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
		BasePage: basePage(r),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Log:      log,
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
		BasePage: basePage(r),
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
		BasePage:    basePage(r),
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

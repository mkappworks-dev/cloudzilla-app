package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/model"
)

func (h *Handler) PageHome(w http.ResponseWriter, r *http.Request) {
	repos, err := h.Services.Repo.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list repos", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []model.Repository{}
	}
	h.render(w, "home", HomeData{Repos: repos})
}

func (h *Handler) PageLogin(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login", LoginData{})
}

func (h *Handler) PageLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	_, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		h.render(w, "login", LoginData{Error: "Invalid credentials"})
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
		User:  *user,
		Repos: repos,
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

	h.render(w, "repo", RepoData{
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
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
		Repo:     *repo,
		Pull:     *pull,
		Owner:    owner,
		RepoName: repoName,
	})
}

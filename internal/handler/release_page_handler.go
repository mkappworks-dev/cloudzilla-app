package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageReleases(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}

	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = !repo.ContentReadOnly() && h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	rawReleases, err := h.Services.Release.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		slog.Error("releases: list failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "failed to load releases", http.StatusInternalServerError)
		return
	}
	if rawReleases == nil {
		rawReleases = []model.Release{}
	}

	latest, latestErr := h.Services.Release.GetLatest(r.Context(), owner, repoName)
	if latestErr != nil {
		slog.Warn("releases: latest lookup failed", "owner", owner, "repo", repoName, "error", latestErr)
	}

	releaseViews := make([]view.ReleaseView, len(rawReleases))
	for i, rel := range rawReleases {
		rv := view.ReleaseView{Release: rel}
		if latest != nil && rel.ID == latest.ID {
			rv.IsLatest = true
		}
		releaseViews[i] = rv
	}

	h.render(w, r, pages.Releases(view.ReleasesData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "releases", canManage),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Releases: releaseViews,
		CanWrite: canWrite,
	}))
}

func (h *Handler) PageReleaseNew(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := service.CheckContentWritable(repo); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)

	var branches []service.BranchInfo
	if refs, refsErr := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch); refsErr != nil {
		slog.Warn("release new: list refs failed", "owner", owner, "repo", repoName, "error", refsErr)
	} else {
		branches = refs.Branches
	}

	h.render(w, r, pages.ReleaseNew(view.ReleaseNewData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "releases", canManage),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Branches: branches,
	}))
}

func (h *Handler) PageReleaseDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	tagName := unescapeRouted(r, chi.URLParam(r, "tagName"))

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}

	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	release, err := h.Services.Release.GetByTag(r.Context(), owner, repoName, tagName)
	if err != nil {
		http.Error(w, "release not found", http.StatusNotFound)
		return
	}

	authorName := ""
	if u, uErr := h.Services.User.GetByID(r.Context(), release.AuthorID); uErr == nil {
		authorName = u.Username
	} else {
		slog.Warn("release: author lookup failed", "author_id", release.AuthorID, "release_id", release.ID, "error", uErr)
	}

	isLatest := false
	if latest, lErr := h.Services.Release.GetLatest(r.Context(), owner, repoName); lErr == nil && latest != nil {
		isLatest = latest.ID == release.ID
	}

	var commitSHA string
	if commit, _, rErr := h.Services.Code.ResolveRef(owner, repoName, release.TagName); rErr == nil && commit != nil {
		commitSHA = commit.Hash.String()
	} else if rErr != nil {
		slog.Warn("release: resolve tag failed", "owner", owner, "repo", repoName, "tag", release.TagName, "error", rErr)
	}

	h.render(w, h.withAvatars(r, authorName), pages.ReleaseDetail(view.ReleaseDetailData{
		BasePage:   h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "releases", canManage),
		Repo:       *repo,
		Owner:      owner,
		RepoName:   repoName,
		Release:    *release,
		BodyHTML:   markdown.RenderCtx(r.Context(), release.Body),
		CanWrite:   canWrite,
		AuthorName: authorName,
		IsLatest:   isLatest,
		CommitSHA:  commitSHA,
	}))
}

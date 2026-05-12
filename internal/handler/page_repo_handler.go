package handler

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageRepo renders the repository home page with the default branch tree.
func (h *Handler) PageRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
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

	watchLevel := ""
	if currentUserID != 0 {
		watchLevel = h.Services.Watch.GetLevel(r.Context(), currentUserID, repo.ID)
	}

	topics, _ := h.Services.Topic.ListByRepo(r.Context(), repo.ID)
	if topics == nil {
		topics = []model.Topic{}
	}

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
		WatchLevel:    watchLevel,
		ForkCount:     repo.ForkCount,
		IsFork:        repo.IsFork,
		ForkOfPath:    forkOfPath,
		LatestRelease: latestRelease,
		Topics:        topics,
		IsArchived:    repo.IsArchived,
	}))
}

// PageRepoSettings renders the repository settings page with collaborators and webhooks.
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
		h.NotFound(w, r)
		return
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	if !canManage {
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

	// Transfer/delete only for repo owner or org owner (not admin collaborators)
	isOwner := h.Services.Repo.IsOwner(r.Context(), repo, claims.UserID)
	canTransfer := isOwner && repo.OrgID == 0

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
		IsOwner:           isOwner,
		CanTransfer:       canTransfer,
	}))
}

// PageRefs renders the branches and tags overview page.
func (h *Handler) PageRefs(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
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
		h.NotFound(w, r)
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

// PageTree renders the directory tree browser at a given ref and path.
func (h *Handler) PageTree(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "path")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
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
		h.NotFound(w, r)
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

// PageBlob renders a single file with syntax-highlighted numbered lines.
func (h *Handler) PageBlob(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "path")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
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
		h.NotFound(w, r)
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

// PageCommits renders the paginated commit log for a ref.
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
		h.NotFound(w, r)
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
		h.NotFound(w, r)
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

// PageCommit renders the full diff and metadata for a single commit SHA.
func (h *Handler) PageCommit(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	sha := chi.URLParam(r, "sha")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
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
		h.NotFound(w, r)
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

// PageBlame renders the per-line blame annotation for a file at a given ref.
func (h *Handler) PageBlame(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "path")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
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
		h.NotFound(w, r)
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

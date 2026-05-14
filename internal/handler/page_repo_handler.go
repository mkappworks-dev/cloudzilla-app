package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
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

	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, viewerID) {
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
		if errors.Is(err, object.ErrFileNotFound) || errors.Is(err, service.ErrEmptyRepo) {
			continue
		}
		slog.Warn("repo: README lookup failed",
			"owner", owner, "repo", repoName, "candidate", name, "error", err)
	}

	starCount, starErr := h.Services.Star.GetStarCount(r.Context(), repo.ID)
	if starErr != nil {
		slog.Warn("repo: star count failed", "owner", owner, "repo", repoName, "error", starErr)
	}
	isStarred := false
	if currentUserID != 0 {
		var isStarredErr error
		isStarred, isStarredErr = h.Services.Star.IsStarred(r.Context(), repo.ID, currentUserID)
		if isStarredErr != nil {
			slog.Warn("repo: is-starred lookup failed", "owner", owner, "repo", repoName, "user_id", currentUserID, "error", isStarredErr)
		}
	}

	forkOfPath := ""
	if repo.IsFork && repo.ForkOfOwner != "" {
		forkOfPath = repo.ForkOfOwner + "/" + repo.ForkOfName
	}

	latestRelease, releaseErr := h.Services.Release.GetLatest(r.Context(), owner, repoName)
	if releaseErr != nil {
		slog.Warn("repo: latest release lookup failed", "owner", owner, "repo", repoName, "error", releaseErr)
	}

	watchLevel := ""
	if currentUserID != 0 {
		watchLevel = h.Services.Watch.GetLevel(r.Context(), currentUserID, repo.ID)
	}

	topics, topicsErr := h.Services.Topic.ListByRepo(r.Context(), repo.ID)
	if topicsErr != nil {
		slog.Warn("repo: topics list failed", "owner", owner, "repo", repoName, "error", topicsErr)
	}
	if topics == nil {
		topics = []model.Topic{}
	}

	canManage := false
	if currentUserID != 0 {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, currentUserID)
	}

	var languages []components.LangBarItem
	if percents, langErr := h.Services.Language.Percentages(r.Context(), owner, repoName, repo.DefaultBranch); langErr != nil {
		slog.Warn("repo: language percentages failed", "owner", owner, "repo", repoName, "error", langErr)
	} else {
		languages = make([]components.LangBarItem, 0, len(percents))
		for _, p := range percents {
			languages = append(languages, components.LangBarItem{
				Name:    p.Name,
				Percent: p.Percent,
				Color:   components.LangColor(p.Name),
			})
		}
	}
	topContribs, contribErr := h.Services.Repo.TopContributors(r.Context(), owner, repoName, 10)
	if contribErr != nil {
		slog.Warn("repo: top contributors failed", "owner", owner, "repo", repoName, "error", contribErr)
	}
	releases, recentReleasesErr := h.Services.Release.RecentForRepo(r.Context(), owner, repoName, 5)
	if recentReleasesErr != nil {
		slog.Warn("repo: recent releases failed", "owner", owner, "repo", repoName, "error", recentReleasesErr)
	}
	heatmap, heatmapErr := h.Services.CommitStats.LookbackForRepo(r.Context(), repo.ID, 90)
	if heatmapErr != nil {
		slog.Warn("repo: heatmap lookback failed", "owner", owner, "repo", repoName, "error", heatmapErr)
	}

	h.render(w, r, pages.Repo(view.RepoData{
		BasePage:      basePage(r, h.Services),
		Repo:          *repo,
		Owner:         owner,
		RepoName:      repoName,
		CloneHTTP:     cloneHTTP,
		CloneSSH:      cloneSSH,
		CanWrite:      canWrite,
		CanManage:     canManage,
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
		Languages:     languages,
		TopContribs:   topContribs,
		Releases:      releases,
		Heatmap:       heatmap,
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
	path := chi.URLParam(r, "*")

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

	// GetTree resolves the ref and produces breadcrumbs; we drop its Entries
	// and re-fetch them enriched with last-commit metadata below.
	result, err := h.Services.Code.GetTree(owner, repoName, ref, path)
	if err != nil {
		if errors.Is(err, service.ErrEmptyRepo) {
			result = &service.TreeResult{Ref: ref, Path: path}
		} else {
			h.NotFound(w, r)
			return
		}
	}

	entries, err := h.Services.Code.ListEntriesWithLastCommit(r.Context(), owner, repoName, result.Ref, result.Path)
	if err != nil {
		if errors.Is(err, service.ErrEmptyRepo) {
			entries = nil
		} else {
			h.NotFound(w, r)
			return
		}
	}

	// Sort directories first, then files; both alphabetically — preserves
	// the ordering GetTree used to provide.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})

	// Latest commit summary across entries in the current dir.
	var newest service.TreeEntryWithLastCommit
	for _, e := range entries {
		if e.LastCommit.Timestamp.After(newest.LastCommit.Timestamp) {
			newest = e
		}
	}
	var latestCommit view.TreeLatestCommit
	if newest.LastCommit.SHA != "" {
		short := newest.LastCommit.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		latestCommit = view.TreeLatestCommit{
			SHA:       short,
			Message:   newest.LastCommit.Message,
			Author:    newest.LastCommit.Author,
			AuthorURL: "/" + newest.LastCommit.Author,
			CommitURL: "/" + owner + "/" + repoName + "/commit/" + newest.LastCommit.SHA,
			Timestamp: newest.LastCommit.Timestamp,
		}
	}

	// IDE-style sidebar nodes for the current directory.
	sidebar := make([]components.TreeNode, 0, len(entries))
	for _, e := range entries {
		href := "/" + owner + "/" + repoName + "/blob/" + result.Ref + "/" + e.Path
		if e.IsDir {
			href = "/" + owner + "/" + repoName + "/tree/" + result.Ref + "/" + e.Path
		}
		sidebar = append(sidebar, components.TreeNode{
			Name:  e.Name,
			IsDir: e.IsDir,
			Href:  href,
		})
	}

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Tree(view.TreeData{
		BasePage:     basePage(r, h.Services),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		Ref:          result.Ref,
		Path:         result.Path,
		Breadcrumbs:  result.Breadcrumbs,
		Entries:      entries,
		RefsURL:      "/" + owner + "/" + repoName + "/refs",
		CanManage:    canManage,
		Sidebar:      sidebar,
		LatestCommit: latestCommit,
	}))
}

// PageBlob renders a single file with syntax-highlighted numbered lines.
func (h *Handler) PageBlob(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "*")

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

	var canWrite, canManage bool
	if userID != nil {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, *userID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, *userID)
	}

	// Latest commit touching this specific file path. Best-effort: failures
	// just leave the sub-header off.
	var latestCommit view.TreeLatestCommit
	if last, lcErr := h.Services.Code.LastCommitForPath(r.Context(), owner, repoName, result.Ref, result.Path); lcErr == nil && last != nil {
		full := last.Hash.String()
		short := full
		if len(short) > 7 {
			short = short[:7]
		}
		latestCommit = view.TreeLatestCommit{
			SHA:       short,
			Message:   firstCommitLine(last.Message),
			Author:    last.Author.Name,
			AuthorURL: "/" + last.Author.Name,
			CommitURL: "/" + owner + "/" + repoName + "/commit/" + full,
			Timestamp: last.Author.When,
		}
	}

	h.render(w, r, pages.Blob(view.BlobData{
		BasePage:     basePage(r, h.Services),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		Ref:          result.Ref,
		Path:         result.Path,
		Breadcrumbs:  result.Breadcrumbs,
		Lines:        result.Lines,
		IsBinary:     result.IsBinary,
		Size:         result.Size,
		BlameURL:     result.BlameURL,
		RawURL:       "/" + owner + "/" + repoName + "/raw/" + result.Ref + "/" + result.Path,
		EditURL:      "#",
		CanWrite:     canWrite,
		CanManage:    canManage,
		LatestCommit: latestCommit,
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
	path := chi.URLParam(r, "*")

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

	authors := make(map[string]struct{}, len(result.Lines))
	for _, l := range result.Lines {
		authors[l.Author] = struct{}{}
	}

	var canManage bool
	if userID != nil {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, *userID)
	}

	h.render(w, r, pages.Blame(view.BlameData{
		BasePage:     basePage(r, h.Services),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		Ref:          result.Ref,
		Path:         result.Path,
		Breadcrumbs:  result.Breadcrumbs,
		Lines:        result.Lines,
		BlobURL:      result.BlobURL,
		Contributors: len(authors),
		CanManage:    canManage,
	}))
}

// firstCommitLine returns the first line of a commit message (subject only).
func firstCommitLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

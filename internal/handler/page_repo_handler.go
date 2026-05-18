package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

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

	var readmeHTML, readmeName string
	for _, name := range []string{"README.md", "readme.md", "Readme.md"} {
		raw, err := h.Services.Code.GetRawBlob(owner, repoName, repo.DefaultBranch, name)
		if err == nil {
			readmeHTML = markdown.Render(string(raw))
			readmeName = name
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
	topContribs, contribErr := h.Services.Repo.TopContributors(r.Context(), owner, repoName, repo.DefaultBranch, 10)
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

	var repoEntries []service.TreeEntryWithLastCommit
	var repoLatestCommit view.TreeLatestCommit
	if entries, lcErr := h.Services.Code.ListEntriesWithLastCommit(r.Context(), owner, repoName, repo.DefaultBranch, ""); lcErr == nil {
		repoEntries = entries
		var newest service.TreeEntryWithLastCommit
		for _, e := range entries {
			if e.LastCommit.Timestamp.After(newest.LastCommit.Timestamp) {
				newest = e
			}
		}
		if newest.LastCommit.SHA != "" {
			short := newest.LastCommit.SHA
			if len(short) > 7 {
				short = short[:7]
			}
			repoLatestCommit = view.TreeLatestCommit{
				SHA:       short,
				Message:   newest.LastCommit.Message,
				Author:    newest.LastCommit.Author,
				AuthorURL: "/" + newest.LastCommit.Author,
				CommitURL: "/" + owner + "/" + repoName + "/commit/" + newest.LastCommit.SHA,
				Timestamp: newest.LastCommit.Timestamp,
			}
		}
	} else if !errors.Is(lcErr, service.ErrEmptyRepo) {
		slog.Warn("repo: entries lookup failed", "owner", owner, "repo", repoName, "error", lcErr)
	}

	var branches []service.BranchInfo
	var tags []service.TagInfo
	if refs, refsErr := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch); refsErr != nil {
		slog.Warn("repo: list refs failed", "owner", owner, "repo", repoName, "error", refsErr)
	} else {
		branches = refs.Branches
		tags = refs.Tags
	}

	commitCount := 0
	if n, ccErr := h.Services.Code.CommitCount(owner, repoName, repo.DefaultBranch); ccErr != nil {
		slog.Warn("repo: commit count failed", "owner", owner, "repo", repoName, "error", ccErr)
	} else {
		commitCount = n
	}

	var allFiles []string
	if files, afErr := h.Services.Code.ListAllFiles(owner, repoName, repo.DefaultBranch); afErr != nil {
		slog.Warn("repo: list all files failed", "owner", owner, "repo", repoName, "error", afErr)
	} else {
		allFiles = files
	}

	h.render(w, r, pages.Repo(view.RepoData{
		BasePage:      h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
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
		Entries:       repoEntries,
		LatestCommit:  repoLatestCommit,
		ReadmeName:    readmeName,
		Branches:      branches,
		Tags:          tags,
		BranchCount:   len(branches),
		TagCount:      len(tags),
		CommitCount:   commitCount,
		AllFiles:      allFiles,
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
		BasePage:          h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "settings", canManage),
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

// UpdateRepoGeneral handles the settings page's General-section form:
// description, website, and default branch.
func (h *Handler) UpdateRepoGeneral(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.Services.Repo.UpdateGeneral(r.Context(), repo.ID, claims.UserID,
		r.FormValue("description"), r.FormValue("website"), r.FormValue("default_branch")); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, "you do not have permission to change these settings", http.StatusForbidden)
			return
		}
		slog.Error("settings: update general failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "failed to update settings", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/settings", http.StatusSeeOther)
}

// UpdateRepoFeatures handles the settings page's Access-section feature
// toggles: allow Issues / Discussions / Projects / Wiki.
func (h *Handler) UpdateRepoFeatures(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.Services.Repo.UpdateFeatureToggles(r.Context(), repo.ID, claims.UserID,
		r.FormValue("allow_issues") == "on",
		r.FormValue("allow_discussions") == "on",
		r.FormValue("allow_projects") == "on",
		r.FormValue("allow_wiki") == "on"); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, "you do not have permission to change these settings", http.StatusForbidden)
			return
		}
		slog.Error("settings: update feature toggles failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "failed to update settings", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/settings", http.StatusSeeOther)
}

// UpdateRepoVisibility handles POST /{owner}/{repo}/settings/visibility.
func (h *Handler) UpdateRepoVisibility(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.Services.Repo.UpdateVisibility(r.Context(), repo.ID, claims.UserID,
		r.FormValue("private") == "true"); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, "you do not have permission to change these settings", http.StatusForbidden)
			return
		}
		slog.Error("settings: update visibility failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "failed to update settings", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/settings", http.StatusSeeOther)
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
	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Refs(view.RefsData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
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
	result, treeErr := h.Services.Code.GetTree(owner, repoName, ref, path)

	// When GetTree fails on a non-empty path, the path may be a file rather
	// than a directory. Try GetBlob and render the file inline in the tree page.
	if treeErr != nil && !errors.Is(treeErr, service.ErrEmptyRepo) && path != "" {
		blobResult, blobErr := h.Services.Code.GetBlob(owner, repoName, ref, path)
		if blobErr == nil {
			slog.Debug("tree: fell back to blob render",
				"owner", owner, "repo", repoName, "ref", ref, "path", path, "tree_err", treeErr)
			parentPath := ""
			fileName := path
			if idx := strings.LastIndex(path, "/"); idx >= 0 {
				parentPath = path[:idx]
				fileName = path[idx+1:]
			}

			canWrite := userID != nil && h.Services.Repo.CanWrite(r.Context(), repo, *userID)
			canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

			var latestCommit view.TreeLatestCommit
			last, lcErr := h.Services.Code.LastCommitForPath(r.Context(), owner, repoName, blobResult.Ref, blobResult.Path)
			if lcErr != nil {
				slog.Warn("tree: blob-fallback LastCommitForPath failed",
					"owner", owner, "repo", repoName, "ref", blobResult.Ref, "path", blobResult.Path, "error", lcErr)
			}
			if lcErr == nil && last != nil {
				full := last.Hash.String()
				short := full
				if len(short) > 7 {
					short = short[:7]
				}
				latestCommit = view.TreeLatestCommit{
					SHA:       short,
					Message:   service.FirstLine(last.Message),
					Author:    last.Author.Name,
					AuthorURL: "/" + last.Author.Name,
					CommitURL: "/" + owner + "/" + repoName + "/commit/" + full,
					Timestamp: last.Author.When,
				}
			}

			h.render(w, r, pages.Tree(view.TreeData{
				BasePage:     h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
				Repo:         *repo,
				Owner:        owner,
				RepoName:     repoName,
				Ref:          blobResult.Ref,
				Path:         blobResult.Path,
				Breadcrumbs:  blobResult.Breadcrumbs,
				RefsURL:      "/" + owner + "/" + repoName + "/refs",
				CanManage:    canManage,
				Sidebar:      h.buildSidebarTree(owner, repoName, blobResult.Ref, parentPath),
				LatestCommit: latestCommit,
				ActiveFile:   fileName,
				FileView: &view.TreeFileView{
					Lines:    blobResult.Lines,
					IsBinary: blobResult.IsBinary,
					Size:     blobResult.Size,
					FileName: fileName,
					BlameURL: blobResult.BlameURL,
					RawURL:   "/" + owner + "/" + repoName + "/raw/" + blobResult.Ref + "/" + blobResult.Path,
					EditURL:  "#",
					CanWrite: canWrite,
				},
			}))
			return
		}
		slog.Warn("tree: blob fallback also failed",
			"owner", owner, "repo", repoName, "ref", ref, "path", path,
			"tree_err", treeErr, "blob_err", blobErr)
	}

	if treeErr != nil {
		if errors.Is(treeErr, service.ErrEmptyRepo) {
			result = &service.TreeResult{Ref: ref, Path: path}
		} else {
			slog.Warn("tree: GetTree failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", treeErr)
			h.NotFound(w, r)
			return
		}
	}

	entries, err := h.Services.Code.ListEntriesWithLastCommit(r.Context(), owner, repoName, result.Ref, result.Path)
	if err != nil {
		if errors.Is(err, service.ErrEmptyRepo) {
			entries = nil
		} else {
			slog.Warn("tree: ListEntriesWithLastCommit failed", "owner", owner, "repo", repoName, "ref", result.Ref, "path", result.Path, "error", err)
			h.NotFound(w, r)
			return
		}
	}

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

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Tree(view.TreeData{
		BasePage:     h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		Ref:          result.Ref,
		Path:         result.Path,
		Breadcrumbs:  result.Breadcrumbs,
		Entries:      entries,
		RefsURL:      "/" + owner + "/" + repoName + "/refs",
		CanManage:    canManage,
		Sidebar:      h.buildSidebarTree(owner, repoName, result.Ref, result.Path),
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
	last, lcErr := h.Services.Code.LastCommitForPath(r.Context(), owner, repoName, result.Ref, result.Path)
	if lcErr != nil {
		slog.Warn("blob: LastCommitForPath failed", "owner", owner, "repo", repoName, "ref", result.Ref, "path", result.Path, "error", lcErr)
	}
	if lcErr == nil && last != nil {
		full := last.Hash.String()
		short := full
		if len(short) > 7 {
			short = short[:7]
		}
		latestCommit = view.TreeLatestCommit{
			SHA:       short,
			Message:   service.FirstLine(last.Message),
			Author:    last.Author.Name,
			AuthorURL: "/" + last.Author.Name,
			CommitURL: "/" + owner + "/" + repoName + "/commit/" + full,
			Timestamp: last.Author.When,
		}
	}

	h.render(w, r, pages.Blob(view.BlobData{
		BasePage:     h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
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

func (h *Handler) PageCommitsRedirect(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/commits/"+repo.DefaultBranch, http.StatusFound)
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
		switch {
		case errors.Is(err, service.ErrEmptyRepo):
			log = &service.CommitLog{Ref: ref, Page: page}
		case errors.Is(err, service.ErrRefNotFound):
			h.NotFound(w, r)
			return
		default:
			slog.Error("PageCommits: GetCommits failed",
				"owner", owner, "repo", repoName, "ref", ref, "page", page, "error", err)
			http.Error(w, "failed to load commits", http.StatusInternalServerError)
			return
		}
	}

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Commits(view.CommitsData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
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

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Commit(view.CommitData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
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
		BasePage:     h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
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

// buildSidebarTree returns the root tree with the path to currentPath expanded.
func (h *Handler) buildSidebarTree(owner, repoName, ref, currentPath string) []components.TreeNode {
	root, err := h.Services.Code.GetTree(owner, repoName, ref, "")
	if err != nil {
		slog.Warn("sidebar: root GetTree failed",
			"owner", owner, "repo", repoName, "ref", ref, "error", err)
		return nil
	}
	var segs []string
	if currentPath != "" {
		segs = strings.Split(currentPath, "/")
	}
	return h.buildSidebarLevel(owner, repoName, ref, "", root.Entries, segs)
}

// buildSidebarLevel recursively expands the directory matching remainingPath[0].
func (h *Handler) buildSidebarLevel(owner, repoName, ref, dirPath string, entries []service.TreeEntry, remainingPath []string) []components.TreeNode {
	nodes := make([]components.TreeNode, 0, len(entries))
	for _, e := range entries {
		var entryPath string
		if dirPath == "" {
			entryPath = e.Name
		} else {
			entryPath = dirPath + "/" + e.Name
		}
		kind := "tree"
		if !e.IsDir {
			kind = "blob"
		}
		href := "/" + owner + "/" + repoName + "/" + kind + "/" + ref + "/" + entryPath
		node := components.TreeNode{Name: e.Name, IsDir: e.IsDir, Href: href}
		if e.IsDir && len(remainingPath) > 0 && e.Name == remainingPath[0] {
			node.IsOpen = true
			child, err := h.Services.Code.GetTree(owner, repoName, ref, entryPath)
			if err != nil {
				slog.Warn("sidebar: child GetTree failed",
					"owner", owner, "repo", repoName, "ref", ref, "path", entryPath, "error", err)
			} else {
				node.Children = h.buildSidebarLevel(owner, repoName, ref, entryPath, child.Entries, remainingPath[1:])
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

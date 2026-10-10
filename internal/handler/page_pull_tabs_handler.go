package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PagePullCommits(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		http.Error(w, "invalid pull number", http.StatusBadRequest)
		return
	}

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

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	var loadErrCommits bool
	commits, err := h.Services.Code.PullCommits(owner, repoName, pull.BaseBranch, pull.HeadBranch)
	if err != nil {
		slog.Warn("pull commits: git walk failed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
		commits = nil
		loadErrCommits = true
	}

	var authorUsername string
	if author, err := h.Services.User.GetByID(r.Context(), pull.AuthorID); err == nil {
		authorUsername = author.Username
	} else {
		slog.Warn("pull commits: author lookup failed; falling back to name",
			"owner", owner, "repo", repoName, "pull_number", number, "author_id", pull.AuthorID, "error", err)
	}

	canManage := false
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	chromeCounts := h.pullChromeCounts(r.Context(), owner, repoName, pull, commitsBadge)
	chromeCounts.CommitsCount = len(commits)

	h.render(w, r, pages.PullCommits(view.PullCommitsData{
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
		OwnerName:        owner,
		Repo:             repo,
		Pull:             pull,
		AuthorUsername:   authorUsername,
		Commits:          commits,
		CanWrite:         canWrite,
		PullChromeCounts: chromeCounts,
		LoadError:        loadErrCommits,
	}))
}

func (h *Handler) PagePullChecks(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		http.Error(w, "invalid pull number", http.StatusBadRequest)
		return
	}

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

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	var rows []components.CheckRow
	var loadErrChecks bool
	var headSHA string
	headCommit, _, rerr := h.Services.Code.ResolveRef(owner, repoName, pull.HeadBranch)
	if rerr != nil {
		slog.Warn("pull checks: ref resolution failed", "owner", owner, "repo", repoName, "pull_number", number, "error", rerr)
		loadErrChecks = true
	} else {
		headSHA = headCommit.Hash.String()
		statuses, serr := h.Services.CommitStatus.List(r.Context(), owner, repoName, headSHA)
		if serr != nil {
			slog.Warn("pull checks: status list failed", "owner", owner, "repo", repoName, "pull_number", number, "error", serr)
			loadErrChecks = true
		} else {
			rows = make([]components.CheckRow, 0, len(statuses))
			for _, s := range statuses {
				rows = append(rows, components.CheckRow{
					Context:     s.Context,
					State:       string(s.State),
					Description: s.Description,
					URL:         s.TargetURL,
				})
			}
		}
	}

	var authorUsername string
	if author, err := h.Services.User.GetByID(r.Context(), pull.AuthorID); err == nil {
		authorUsername = author.Username
	} else {
		slog.Warn("pull checks: author lookup failed; falling back to name",
			"owner", owner, "repo", repoName, "pull_number", number, "author_id", pull.AuthorID, "error", err)
	}

	canManage := false
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	chromeCounts := h.pullChromeCounts(r.Context(), owner, repoName, pull, checksBadge)
	chromeCounts.ChecksTotal = len(rows)
	for _, row := range rows {
		if row.State == string(model.CommitStatusSuccess) {
			chromeCounts.ChecksPassed++
		}
	}

	h.render(w, r, pages.PullChecks(view.PullChecksData{
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
		OwnerName:        owner,
		Repo:             repo,
		Pull:             pull,
		AuthorUsername:   authorUsername,
		Rows:             rows,
		HeadSHA:          headSHA,
		CanWrite:         canWrite,
		PullChromeCounts: chromeCounts,
		LoadError:        loadErrChecks,
	}))
}

func (h *Handler) PagePullFiles(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		http.Error(w, "invalid pull number", http.StatusBadRequest)
		return
	}

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

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	var loadErrFiles bool
	diff, err := h.Services.Code.GetPullDiff(owner, repoName, pull.BaseBranch, pull.HeadBranch)
	if err != nil {
		slog.Warn("pull files: get pull diff failed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
		diff = &service.PRDiffResult{}
		loadErrFiles = true
	} else {
		h.Services.Code.HighlightDiffs(owner, repoName, diff.Files)
	}

	// Anchor index must match the pr_files template's section id="diff-N" — both
	// iterate Diff.Files in order, so sidebar links resolve to the right section.
	tree := make([]components.DiffFileTreeItem, 0, len(diff.Files))
	for i, f := range diff.Files {
		tree = append(tree, components.DiffFileTreeItem{
			Path:    f.DisplayPath(),
			Anchor:  fmt.Sprintf("diff-%d", i),
			Added:   f.Added,
			Deleted: f.Deleted,
		})
	}

	rawLineComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
	lineComments := map[view.LineCommentKey][]RenderedLineComment{}
	for _, c := range rawLineComments {
		key := view.LineCommentKeyOf(c)
		lineComments[key] = append(lineComments[key], RenderedLineComment{
			PullLineComment: c,
			BodyHTML:        markdown.RenderCtx(r.Context(), c.Body),
		})
	}

	var authorUsername string
	if author, err := h.Services.User.GetByID(r.Context(), pull.AuthorID); err == nil {
		authorUsername = author.Username
	} else {
		slog.Warn("pull files: author lookup failed; falling back to name",
			"owner", owner, "repo", repoName, "pull_number", number, "author_id", pull.AuthorID, "error", err)
	}

	canManage := false
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	chromeCounts := h.pullChromeCounts(r.Context(), owner, repoName, pull, filesBadge)
	chromeCounts.FilesCount = len(diff.Files)
	chromeCounts.Added = diff.TotalAdded
	chromeCounts.Deleted = diff.TotalDeleted

	h.render(w, r, pages.PullFiles(view.PullFilesData{
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
		OwnerName:        owner,
		Repo:             repo,
		Pull:             pull,
		AuthorUsername:   authorUsername,
		Tree:             tree,
		Diff:             diff,
		CanWrite:         canWrite,
		LineComments:     lineComments,
		PullChromeCounts: chromeCounts,
		LoadError:        loadErrFiles,
	}))
}

type pullBadge uint8

const (
	convBadge pullBadge = 1 << iota
	commitsBadge
	checksBadge
	filesBadge
)

// pullChromeCounts loads the tab badges not in own. A tab counts its own
// badges from the data its body renders, so the header can't contradict the
// body (or its error banner) and nothing is loaded twice.
func (h *Handler) pullChromeCounts(ctx context.Context, owner, repoName string, pull *model.PullRequest, own pullBadge) view.PullChromeCounts {
	var c view.PullChromeCounts
	logFail := func(what string, err error) {
		slog.Warn("pull chrome counts: "+what+" failed; tab badge may be wrong",
			"owner", owner, "repo", repoName, "pull_number", pull.Number, "error", err)
	}
	if own&commitsBadge == 0 {
		if commits, err := h.Services.Code.PullCommits(owner, repoName, pull.BaseBranch, pull.HeadBranch); err == nil {
			c.CommitsCount = len(commits)
		} else {
			logFail("commit walk", err)
		}
	}
	if own&checksBadge == 0 {
		if headCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pull.HeadBranch); err == nil {
			if statuses, err := h.Services.CommitStatus.List(ctx, owner, repoName, headCommit.Hash.String()); err == nil {
				c.ChecksTotal, c.ChecksPassed = checkCounts(statuses)
			} else {
				logFail("status list", err)
			}
		} else {
			logFail("ref resolution", err)
		}
	}
	if own&filesBadge == 0 {
		if stats, err := h.Services.Code.PullDiffStats(owner, repoName, pull.BaseBranch, pull.HeadBranch); err == nil {
			c.FilesCount = stats.Files
			c.Added = stats.Added
			c.Deleted = stats.Deleted
		} else {
			logFail("pull diff stats", err)
		}
	}
	if own&convBadge == 0 {
		if comments, err := h.Services.Comment.ListByPull(ctx, pull.ID); err == nil {
			c.ConvCount += len(comments)
		} else {
			logFail("comment list", err)
		}
		if reviews, err := h.Services.PullReview.ListByPull(ctx, owner, repoName, pull.Number); err == nil {
			c.ConvCount += submittedReviewCount(reviews)
		} else {
			logFail("review list", err)
		}
	}
	return c
}

func checkCounts(statuses []model.CommitStatus) (total, passed int) {
	for _, s := range statuses {
		if s.State == model.CommitStatusSuccess {
			passed++
		}
	}
	return len(statuses), passed
}

func submittedReviewCount(reviews []model.PullReview) int {
	n := 0
	for _, rv := range reviews {
		if rv.State != model.PRReviewPending {
			n++
		}
	}
	return n
}

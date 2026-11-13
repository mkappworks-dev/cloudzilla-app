package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"
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
	var commits []service.CommitSummary
	base, head, rangeKnown := pullDiffRefs(pull)
	if rangeKnown {
		commits, err = h.Services.Code.PullCommits(owner, repoName, base, head)
		if err != nil {
			slog.Warn("pull commits: git walk failed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
			commits = nil
			loadErrCommits = true
		}
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

	chromeCounts := h.pullChromeCounts(r.Context(), repo, owner, repoName, pull, commitsBadge)
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
		Unavailable:      !rangeKnown,
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

	var statuses []model.CommitStatus
	var loadErrChecks bool
	var headSHA string
	headCommit, _, rerr := h.Services.Code.ResolveRef(owner, repoName, pull.HeadRef())
	if rerr != nil {
		slog.Warn("pull checks: ref resolution failed", "owner", owner, "repo", repoName, "pull_number", number, "error", rerr)
		loadErrChecks = true
	} else {
		headSHA = headCommit.Hash.String()
		var serr error
		statuses, serr = h.Services.CommitStatus.List(r.Context(), owner, repoName, headSHA)
		if serr != nil {
			slog.Warn("pull checks: status list failed", "owner", owner, "repo", repoName, "pull_number", number, "error", serr)
			loadErrChecks = true
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

	runs, rows := h.checkRuns(r.Context(), repo, statuses, canWrite)
	chromeCounts := h.pullChromeCounts(r.Context(), repo, owner, repoName, pull, checksBadge)
	chromeCounts.ChecksTotal, chromeCounts.ChecksPassed = checkCounts(statuses)

	h.render(w, r, pages.PullChecks(view.PullChecksData{
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
		OwnerName:        owner,
		Repo:             repo,
		Pull:             pull,
		AuthorUsername:   authorUsername,
		Runs:             runs,
		Rows:             rows,
		States:           statusStates(statuses),
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
	diff := &service.PRDiffResult{}
	base, head, rangeKnown := pullDiffRefs(pull)
	if rangeKnown {
		diff, err = h.Services.Code.GetPullDiff(owner, repoName, base, head)
		if err != nil {
			slog.Warn("pull files: get pull diff failed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
			diff = &service.PRDiffResult{}
			loadErrFiles = true
		} else {
			h.Services.Code.HighlightDiffs(owner, repoName, diff.Files)
		}
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
	for _, v := range h.lineCommentViews(r.Context(), owner, repoName, pull.HeadRef(), rawLineComments) {
		key := view.LineCommentKeyOf(v.PullLineComment)
		lineComments[key] = append(lineComments[key], v)
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

	chromeCounts := h.pullChromeCounts(r.Context(), repo, owner, repoName, pull, filesBadge)
	chromeCounts.FilesCount = len(diff.Files)
	chromeCounts.Added = diff.TotalAdded
	chromeCounts.Deleted = diff.TotalDeleted

	h.render(w, h.withAvatars(r, lineCommentAuthors(rawLineComments)...), pages.PullFiles(view.PullFilesData{
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
		Unavailable:      !rangeKnown,
	}))
}

// pullDiffRefs are the refs a PR's commits and diff are read from: its branches, or for a merged PR the tips the merge
// saw, since afterwards the branches show nothing. known is false for a merged PR with no recorded range.
func pullDiffRefs(pull *model.PullRequest) (base, head string, known bool) {
	if pull.State != model.PRStateMerged {
		return pull.BaseBranch, pull.HeadRef(), true
	}
	return pull.MergedBaseSHA, pull.MergedHeadSHA, pull.MergedHeadSHA != ""
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
func (h *Handler) pullChromeCounts(ctx context.Context, repo *model.Repository, owner, repoName string, pull *model.PullRequest, own pullBadge) view.PullChromeCounts {
	var c view.PullChromeCounts
	logFail := func(what string, err error) {
		slog.Warn("pull chrome counts: "+what+" failed; tab badge may be wrong",
			"owner", owner, "repo", repoName, "pull_number", pull.Number, "error", err)
	}
	if pull.State == model.PRStateMerged {
		if events, err := h.Services.PullEvent.ListByPull(ctx, pull.ID); err == nil {
			for _, ev := range slices.Backward(events) {
				if ev.Type == model.PullEventMerged {
					c.MergedBy = ev.ActorName
					c.MergedCommits, _ = strconv.Atoi(ev.Detail)
					break
				}
			}
		} else {
			logFail("event list", err)
		}
	}
	base, head, rangeKnown := pullDiffRefs(pull)
	if own&commitsBadge == 0 && rangeKnown {
		if commits, err := h.Services.Code.PullCommitsUnverified(owner, repoName, base, head); err == nil {
			c.CommitsCount = len(commits)
		} else {
			logFail("commit walk", err)
		}
	}
	headSHA := pull.MergedHeadSHA
	if pull.State != model.PRStateMerged {
		if headCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pull.HeadRef()); err == nil {
			headSHA = headCommit.Hash.String()
		} else {
			logFail("ref resolution", err)
		}
	}
	if headSHA != "" {
		c.HeadCI = h.ciBadge(ctx, repo, headSHA)
		if own&checksBadge == 0 {
			if statuses, err := h.Services.CommitStatus.List(ctx, owner, repoName, headSHA); err == nil {
				c.ChecksTotal, c.ChecksPassed = checkCounts(statuses)
			} else {
				logFail("status list", err)
			}
		}
	}
	if own&filesBadge == 0 && rangeKnown {
		if stats, err := h.Services.Code.PullDiffStats(owner, repoName, base, head); err == nil {
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

func statusStates(statuses []model.CommitStatus) []string {
	out := make([]string, len(statuses))
	for i, s := range statuses {
		out[i] = string(s.State)
	}
	return out
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

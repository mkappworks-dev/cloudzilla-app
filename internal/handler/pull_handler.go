package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// crossRepoUpdateMessage explains why a pull request from another repository's branch can't be changed here.
const crossRepoUpdateMessage = "the head branch is in another repository: update it there"

type createPRRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	HeadBranch string `json:"head_branch"`
	BaseBranch string `json:"base_branch"`
	IsDraft    bool   `json:"is_draft"`
	// HeadRepo is "owner/name" of the repository holding HeadBranch; empty means the base itself.
	HeadRepo string `json:"head_repo"`
}

type updatePRRequest struct {
	State             string  `json:"state"`
	Title             *string `json:"title"`
	Body              *string `json:"body"`
	MergeStrategy     string  `json:"merge_strategy"` // "ff" | "merge" | "squash"
	MergeTitle        string  `json:"merge_title"`
	MergeMessage      string  `json:"merge_message"`
	IsDraft           *bool   `json:"is_draft"`
	AutoMerge         string  `json:"auto_merge"`          // "enable" | "disable"
	AutoMergeStrategy string  `json:"auto_merge_strategy"` // "ff" | "merge" | "squash"
}

var autoMergeAuthor = service.GitAuthor{Name: "auto-merge", Email: "auto-merge@localhost"}

func (h *Handler) ListPulls(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	if _, ok := h.readableRepoJSON(w, r, owner, repo); !ok {
		return
	}
	prs, err := h.Services.Pull.List(r.Context(), owner, repo)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	writeJSON(w, http.StatusOK, prs)
}

func (h *Handler) GetPull(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	if _, ok := h.readableRepoJSON(w, r, owner, repo); !ok {
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull number")
		return
	}
	pr, err := h.Services.Pull.Get(r.Context(), owner, repo, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	h.hidePullHead(r, pr)
	writeJSON(w, http.StatusOK, pr)
}

func (h *Handler) CreatePull(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}

	var req createPRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pr, err := h.Services.Pull.CreateFromHead(r.Context(), owner, repoName, req.HeadRepo, claims.UserID, req.Title, req.Body, req.HeadBranch, req.BaseBranch, req.IsDraft, claims.Targets)
	if err != nil {
		if errors.Is(err, service.ErrPullForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, service.ErrPullIntoMirror) {
			writeError(w, http.StatusUnprocessableEntity, pullIntoMirrorMessage)
			return
		}
		if errors.Is(err, service.ErrPullHeadNotInTree) || errors.Is(err, service.ErrPullHeadBranchMissing) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		slog.Error("create pull: service create failed",
			"owner", owner, "repo", repoName, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload("opened", *repo, *pr))
	repoID := repo.ID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventPROpened, map[string]any{"number": pr.Number, "title": pr.Title})

	go func(owner, repoName string, pr *model.PullRequest, defaultBranch string) {
		diff, err := h.Services.Code.GetPullDiff(owner, repoName, pr.BaseBranch, pr.HeadRef())
		if err != nil {
			return
		}
		changedFiles := make([]string, 0, len(diff.Files))
		for _, f := range diff.Files {
			changedFiles = append(changedFiles, f.NewPath)
		}
		rules, err := h.Services.Code.GetCodeOwners(owner, repoName, defaultBranch)
		if err != nil || len(rules) == 0 {
			return
		}
		owners := h.Services.Code.MatchCodeOwners(rules, changedFiles)
		for _, u := range owners {
			_ = h.Services.Assignee.AddToPull(context.Background(), owner, repoName, pr.Number, u)
		}
	}(owner, repoName, pr, repo.DefaultBranch)

	writeJSON(w, http.StatusCreated, pr)
}

func (h *Handler) UpdatePull(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull request number")
		return
	}

	repo, ok := h.writableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	var state, prTitle, mergeStrategy, mergeTitle, mergeMessage, isDraftStr, autoMergeAction, autoMergeStrategy string
	var titlePresent bool
	var req *updatePRRequest
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = r.FormValue("state")
		prTitle = r.FormValue("title")
		titlePresent = r.PostForm.Has("title")
		mergeStrategy = r.FormValue("merge_strategy")
		mergeTitle = r.FormValue("merge_title")
		mergeMessage = r.FormValue("merge_message")
		isDraftStr = r.FormValue("is_draft")
		autoMergeAction = r.FormValue("auto_merge")
		autoMergeStrategy = r.FormValue("auto_merge_strategy")
	} else {
		var decoded updatePRRequest
		if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req = &decoded
		state = req.State
		if req.Title != nil {
			prTitle = *req.Title
			titlePresent = true
		}
		mergeStrategy = req.MergeStrategy
		mergeTitle = req.MergeTitle
		mergeMessage = req.MergeMessage
		autoMergeAction = req.AutoMerge
		autoMergeStrategy = req.AutoMergeStrategy
	}
	if mergeStrategy == "" {
		mergeStrategy = "ff"
	}

	// Validate state if provided
	if state != "" && state != "open" && state != "closed" && state != "merged" {
		writeError(w, http.StatusBadRequest, "state must be 'open', 'closed', or 'merged'")
		return
	}

	// Merging writes the base branch, so pulls:write alone must not do it.
	if (state == "merged" || autoMergeAction == "enable") && !claims.HasScope(model.ScopeRepoWrite) {
		middleware.WriteInsufficientScope(w, model.ScopeRepoWrite)
		return
	}
	if state == "merged" || autoMergeAction == "enable" {
		if err := service.CheckContentWritable(repo); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if titlePresent {
		if prTitle = strings.TrimSpace(prTitle); prTitle == "" {
			writeError(w, http.StatusUnprocessableEntity, "title cannot be empty")
			return
		}
		pr, err := h.Services.Pull.UpdateTitle(r.Context(), owner, repoName, number, prTitle, claims.UserID, claims.Targets)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.recordPullEvent(r.Context(), owner, repoName, pr, claims.UserID, claims.Username, model.PullEventRenamed, prTitle)
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}

	// body may legitimately be empty — detect the edit by field presence.
	if (r.Header.Get("HX-Request") == "true" && r.PostForm.Has("body")) || (req != nil && req.Body != nil) {
		newBody := r.FormValue("body")
		if req != nil && req.Body != nil {
			newBody = *req.Body
		}
		pr, err := h.Services.Pull.UpdateBody(r.Context(), owner, repoName, number, newBody, claims.UserID, claims.Targets)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.recordPullEvent(r.Context(), owner, repoName, pr, claims.UserID, claims.Username, model.PullEventDescribed, "")
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}

	// Handle is_draft toggle
	if isDraftStr != "" || (req != nil && req.IsDraft != nil) {
		newDraft := isDraftStr == "true" || (req != nil && req.IsDraft != nil && *req.IsDraft)
		if err := h.Services.Pull.SetDraft(r.Context(), owner, repoName, number, newDraft); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
		if err != nil {
			slog.Error("update pull: reload after draft toggle failed",
				"owner", owner, "repo", repoName, "pull_number", number, "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		draftEvent := model.PullEventReadied
		if newDraft {
			draftEvent = model.PullEventDrafted
		}
		h.recordPullEvent(r.Context(), owner, repoName, pr, claims.UserID, claims.Username, draftEvent, "")
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}

	// Handle auto-merge enable/disable
	if autoMergeAction != "" {
		var svcErr error
		if autoMergeAction == "enable" {
			if autoMergeStrategy == "" {
				autoMergeStrategy = "ff"
			}
			svcErr = h.Services.Pull.EnableAutoMerge(r.Context(), owner, repoName, number, claims.UserID, autoMergeStrategy)
		} else {
			svcErr = h.Services.Pull.DisableAutoMerge(r.Context(), owner, repoName, number, claims.UserID)
		}
		if svcErr != nil {
			writeError(w, http.StatusUnprocessableEntity, svcErr.Error())
			return
		}
		pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
		if err != nil {
			slog.Error("update pull: reload after auto-merge change failed",
				"owner", owner, "repo", repoName, "pull_number", number, "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}

	if state == "merged" {
		existingPR, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
		if err != nil {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
		actor := service.CloseActor{UserID: claims.UserID, Username: claims.Username, Targets: claims.Targets}
		if len(mergeTitle)+len(mergeMessage) > maxMergeMessage {
			writeError(w, http.StatusUnprocessableEntity, "the merge commit message is too long")
			return
		}
		commitMessage, err := mergeCommitMessage(mergeTitle, mergeMessage)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		pr, err := h.mergePull(r.Context(), repo, existingPR, mergeStrategy, commitMessage, actor, nil)
		var refusal *mergeRefusal
		if errors.As(err, &refusal) {
			writeError(w, refusal.status, refusal.msg)
			return
		}
		if err != nil {
			slog.Error("update pull: merge failed",
				"owner", owner, "repo", repoName, "pull_number", number, "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}

	pr, err := h.Services.Pull.SetState(r.Context(), owner, repoName, number, model.PRState(state))
	if err != nil {
		slog.Error("update pull: set state failed",
			"owner", owner, "repo", repoName, "pull_number", number, "state", state, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if state != "" {
		switch pr.State {
		case model.PRStateClosed:
			h.recordPullEvent(r.Context(), owner, repoName, pr, claims.UserID, claims.Username, model.PullEventClosed, "")
		case model.PRStateOpen:
			h.recordPullEvent(r.Context(), owner, repoName, pr, claims.UserID, claims.Username, model.PullEventReopened, "")
		}
	}

	go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload(state, *repo, *pr))
	go func() {
		h.Services.Notification.NotifyPRStateChange(context.WithoutCancel(r.Context()), *repo, *pr, claims.UserID, claims.Username)
	}()
	repoID := repo.ID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventPRClosed, map[string]any{"number": pr.Number})

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

// UpdatePullBranch merges the base branch into the head branch, so a PR that fell behind can pick up what landed on base.
func (h *Handler) UpdatePullBranch(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull request number")
		return
	}
	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if pr.State != model.PRStateOpen {
		writeError(w, http.StatusUnprocessableEntity, "only an open pull request can be updated")
		return
	}
	if pr.CrossRepo {
		writeError(w, http.StatusUnprocessableEntity, crossRepoUpdateMessage)
		return
	}
	headCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pr.HeadBranch)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the head branch no longer exists")
		return
	}
	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	upd, err := h.Services.Code.UpdatePullBranch(owner, repoName, pr.BaseBranch, pr.HeadBranch, headCommit.Hash, author)
	switch {
	case errors.Is(err, service.ErrRefMoved):
		writeError(w, http.StatusConflict, branchMovedMsg)
		return
	case errors.Is(err, service.ErrBranchUpToDate):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.Services.Push.AfterWebCommit(repo, webCommitActor(claims), upd)

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"head_sha": upd.New.String()})
}

// recordPullEvent appends a timeline event best-effort: a failed write is logged
// with request context but never fails the action that triggered it.
func (h *Handler) recordPullEvent(ctx context.Context, owner, repoName string, pr *model.PullRequest, actorID int64, actorName, eventType, detail string) {
	if err := h.Services.PullEvent.Record(ctx, pr.ID, actorID, actorName, eventType, detail); err != nil {
		slog.Warn("pull event not recorded; timeline will be incomplete",
			"owner", owner, "repo", repoName, "pull_number", pr.Number, "type", eventType, "error", err)
	}
}

type mergeRefusal struct {
	status int
	msg    string
}

func (e *mergeRefusal) Error() string { return e.msg }

const maxMergeMessage = 10000

// mergeCommitMessage joins the merge dialog's title and body; both blank leaves the default message.
func mergeCommitMessage(title, body string) (string, error) {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	switch {
	case title == "" && body == "":
		return "", nil
	case title == "":
		return "", errors.New("the merge commit title can't be blank when a message is given")
	case body == "":
		return title, nil
	}
	return title + "\n\n" + body, nil
}

// mergePull merges pr into its base as actor and does everything a merge
// entails, so no merge path can skip a step. author, when nil, is actor's
// commit identity.
func (h *Handler) mergePull(ctx context.Context, repo *model.Repository, pr *model.PullRequest, strategy, message string, actor service.CloseActor, author *service.GitAuthor) (*model.PullRequest, error) {
	owner, repoName := repo.OwnerName, repo.Name
	if pr.State != model.PRStateOpen {
		return nil, &mergeRefusal{http.StatusUnprocessableEntity, "only an open pull request can be merged"}
	}
	if pr.IsDraft {
		return nil, &mergeRefusal{http.StatusUnprocessableEntity, "cannot merge a draft pull request"}
	}
	if ok, reason, _ := h.Services.PullReview.CanMerge(ctx, pr.ID); !ok {
		return nil, &mergeRefusal{http.StatusUnprocessableEntity, "merge blocked: " + reason}
	}
	var headHash plumbing.Hash
	var headSHA string
	if headCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pr.HeadRef()); err == nil {
		headHash = headCommit.Hash
		headSHA = headHash.String()
	}
	if err := h.Services.BranchProtection.CheckMerge(ctx, repo.ID, pr, headSHA); err != nil {
		return nil, &mergeRefusal{http.StatusUnprocessableEntity, "merge blocked: " + err.Error()}
	}
	if author == nil {
		a, err := h.Services.User.CommitAuthor(ctx, actor.UserID)
		if err != nil {
			return nil, fmt.Errorf("load commit author: %w", err)
		}
		author = &a
	}
	var commitRefs []service.ClosingRef
	if pr.BaseBranch == repo.DefaultBranch && !headHash.IsZero() {
		refs, err := h.Services.Code.ClosingRefsInMerge(owner, repoName, pr.BaseBranch, headHash)
		if err != nil {
			slog.Warn("merge pull: reading closing references failed; commit messages won't close issues",
				"owner", owner, "repo", repoName, "pull_number", pr.Number, "error", err)
		}
		commitRefs = refs
	}

	var baseSHA string
	if baseCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pr.BaseBranch); err == nil {
		baseSHA = baseCommit.Hash.String()
	}

	// Counted before the merge: afterwards a fast-forward or merge commit leaves nothing between base and head.
	mergedCommits := ""
	if commits, err := h.Services.Code.PullCommitsUnverified(owner, repoName, pr.BaseBranch, pr.HeadRef()); err == nil {
		mergedCommits = strconv.Itoa(len(commits))
	}
	if (strategy == "merge" || strategy == "squash") && pr.BaseBranch == repo.DefaultBranch {
		for _, ref := range service.ParseClosingRefs(message) {
			if !slices.Contains(commitRefs, ref) {
				commitRefs = append(commitRefs, ref)
			}
		}
	}

	if message == "" && pr.CrossRepo {
		// The service would name the head ref, which is internal; the fork's branch is what a reader knows.
		head := pr.HeadRepo + ":" + pr.HeadBranch
		switch strategy {
		case "merge":
			message = service.DefaultMergeMessage(head, pr.BaseBranch)
		case "squash":
			message = service.DefaultSquashMessage(head, pr.BaseBranch)
		}
	}

	var err error
	switch strategy {
	case "merge":
		err = h.Services.Code.ThreeWayMergePullRequest(owner, repoName, pr.BaseBranch, pr.HeadRef(), headHash, *author, message)
	case "squash":
		err = h.Services.Code.SquashMergePullRequest(owner, repoName, pr.BaseBranch, pr.HeadRef(), headHash, *author, message)
	default:
		err = h.Services.Code.MergePullRequest(owner, repoName, pr.BaseBranch, pr.HeadRef(), headHash)
	}
	if errors.Is(err, service.ErrRefMoved) {
		return nil, &mergeRefusal{http.StatusConflict, branchMovedMsg}
	}
	if err != nil {
		return nil, &mergeRefusal{http.StatusUnprocessableEntity, err.Error()}
	}

	merged, err := h.Services.Pull.SetState(ctx, owner, repoName, pr.Number, model.PRStateMerged)
	if err != nil {
		return nil, fmt.Errorf("set merged state: %w", err)
	}
	if baseSHA != "" && headSHA != "" {
		if err := h.Services.Pull.RecordMergedRange(ctx, merged.ID, baseSHA, headSHA); err != nil {
			slog.Warn("merge pull: recording the merged range failed; its commits and diff will be unavailable",
				"owner", owner, "repo", repoName, "pull_number", merged.Number, "error", err)
		}
	}
	h.recordPullEvent(ctx, owner, repoName, merged, actor.UserID, actor.Username, model.PullEventMerged, mergedCommits)
	go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload("merged", *repo, *merged))
	bg := context.WithoutCancel(ctx)
	go h.Services.Notification.NotifyPRStateChange(bg, *repo, *merged, actor.UserID, actor.Username)
	repoID := repo.ID
	go h.Services.Event.Record(bg, actor.UserID, actor.Username, &repoID, repoName, owner, model.EventPRMerged, map[string]any{"number": merged.Number})
	h.Services.IssueCloser.CloseForPull(bg, actor, repo, merged, commitRefs)
	h.runPushWorkflows(bg, repo, actor.UserID, pr.BaseBranch, baseSHA)
	return merged, nil
}

// runPushWorkflows creates the `on: push` Runs for the new tip of branch, which a merge moved without a git push;
// the other AfterPush side effects stay out because the merge path already runs its own webhooks and events.
func (h *Handler) runPushWorkflows(ctx context.Context, repo *model.Repository, userID int64, branch, oldSHA string) {
	tip, _, err := h.Services.Code.ResolveRef(repo.OwnerName, repo.Name, branch)
	if err != nil {
		slog.Warn("merge pull: resolve new tip for workflow runs failed", "repo_id", repo.ID, "branch", branch, "error", err)
		return
	}
	cmd := &packp.Command{Name: plumbing.NewBranchReferenceName(branch), New: tip.Hash}
	if oldSHA != "" {
		cmd.Old = plumbing.NewHash(oldSHA)
	}
	go h.Services.WorkflowRun.OnPush(ctx, repo, userID, []*packp.Command{cmd})
}

// tryAutoMerge merges pullID as the user who armed auto-merge, or disarms it
// when that user is unknown or can no longer write the repo.
func (h *Handler) tryAutoMerge(owner, repoName string, pullID int64) {
	ctx := context.Background()

	pr, err := h.Services.Pull.GetByID(ctx, pullID)
	if err != nil {
		return
	}
	if !pr.AutoMergeEnabled || pr.State != model.PRStateOpen || pr.IsDraft {
		return
	}
	repo, err := h.Services.Repo.Get(ctx, owner, repoName)
	if err != nil || service.CheckContentWritable(repo) != nil {
		return
	}

	var user *model.User
	if pr.AutoMergeBy != nil {
		u, err := h.Services.User.GetByID(ctx, *pr.AutoMergeBy)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.Error("auto-merge: load arming user failed", "owner", owner, "repo", repoName, "pull_number", pr.Number, "error", err)
			return
		}
		if err == nil && h.Services.Repo.CanWrite(ctx, repo, u.ID) {
			user = u
		}
	}
	if user == nil {
		if err := h.Services.Pull.DisableAutoMerge(ctx, owner, repoName, pr.Number, 0); err != nil {
			slog.Warn("auto-merge: disarm failed", "owner", owner, "repo", repoName, "pull_number", pr.Number, "error", err)
		}
		return
	}

	actor := service.CloseActor{UserID: user.ID, Username: user.Username}
	author := autoMergeAuthor
	if _, err := h.mergePull(ctx, repo, pr, pr.AutoMergeStrategy, "", actor, &author); err != nil {
		var refusal *mergeRefusal
		if !errors.As(err, &refusal) {
			slog.Error("auto-merge failed", "owner", owner, "repo", repoName, "pull_number", pr.Number, "error", err)
		}
	}
}

// hidePullHead withholds a private head repository's name from a viewer who can't read it.
func (h *Handler) hidePullHead(r *http.Request, pr *model.PullRequest) {
	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	h.Services.Pull.HideUnreadableHead(r.Context(), pr, viewerID)
}

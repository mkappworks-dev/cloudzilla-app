package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageDiscussions renders /{owner}/{repo}/discussions
func (h *Handler) PageDiscussions(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	if !repo.AllowDiscussions {
		http.Error(w, "not found", http.StatusNotFound)
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

	var activeCategoryID int64
	if cidStr := r.URL.Query().Get("category"); cidStr != "" {
		if cid, err := strconv.ParseInt(cidStr, 10, 64); err == nil {
			activeCategoryID = cid
		}
	}

	stateFilter := r.URL.Query().Get("state")
	switch stateFilter {
	case "answered", "closed":
		// valid
	default:
		stateFilter = "open"
	}

	categories, err := h.Services.Discussion.ListCategories(r.Context(), repo.ID)
	if err != nil {
		slog.Warn("discussions: category list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if categories == nil {
		categories = []model.DiscussionCategory{}
	}

	// Unfiltered list for counts; category-filtered list for display rows.
	unfiltered, err := h.Services.Discussion.List(r.Context(), owner, repoName, 0)
	if err != nil {
		slog.Warn("discussions: list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if unfiltered == nil {
		unfiltered = []model.Discussion{}
	}

	var openCount, answeredCount, closedCount int
	categoryCounts := make(map[int64]int)
	for _, d := range unfiltered {
		categoryCounts[d.CategoryID]++
		switch {
		case d.IsAnswered:
			answeredCount++
		case d.IsLocked:
			closedCount++
		default:
			openCount++
		}
	}

	categoryFiltered, err := h.Services.Discussion.List(r.Context(), owner, repoName, activeCategoryID)
	if err != nil {
		slog.Warn("discussions: category-filtered list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if categoryFiltered == nil {
		categoryFiltered = []model.Discussion{}
	}

	var discussions []model.Discussion
	for _, d := range categoryFiltered {
		switch stateFilter {
		case "answered":
			if d.IsAnswered {
				discussions = append(discussions, d)
			}
		case "closed":
			if d.IsLocked && !d.IsAnswered {
				discussions = append(discussions, d)
			}
		default:
			if !d.IsAnswered && !d.IsLocked {
				discussions = append(discussions, d)
			}
		}
	}
	if discussions == nil {
		discussions = []model.Discussion{}
	}

	canWrite := userID != nil && h.Services.Repo.CanWrite(r.Context(), repo, *userID)
	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Discussions(view.DiscussionsData{
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "discussions", canManage),
		Repo:             *repo,
		Owner:            owner,
		RepoName:         repoName,
		Categories:       categories,
		Discussions:      discussions,
		ActiveCategoryID: activeCategoryID,
		CategoryCounts:   categoryCounts,
		TotalCount:       len(unfiltered),
		StateFilter:      stateFilter,
		OpenCount:        openCount,
		AnsweredCount:    answeredCount,
		ClosedCount:      closedCount,
		CanWrite:         canWrite,
	}))
}

// PageDiscussionDetail renders /{owner}/{repo}/discussions/{number}
func (h *Handler) PageDiscussionDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		http.Error(w, "invalid discussion number", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	if !repo.AllowDiscussions {
		http.Error(w, "not found", http.StatusNotFound)
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

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		http.Error(w, "discussion not found", http.StatusNotFound)
		return
	}

	rawReplies, _ := h.Services.Discussion.ListReplies(r.Context(), discussion.ID)
	if rawReplies == nil {
		rawReplies = []model.DiscussionReply{}
	}
	replies := make([]view.RenderedDiscussionReply, len(rawReplies))
	for i, rr := range rawReplies {
		replies[i] = view.RenderedDiscussionReply{
			DiscussionReply: rr,
			BodyHTML:        markdown.Render(rr.Body),
		}
	}

	cats, _ := h.Services.Discussion.ListCategories(r.Context(), repo.ID)
	var category model.DiscussionCategory
	for _, c := range cats {
		if c.ID == discussion.CategoryID {
			category = c
			break
		}
	}

	canWrite := userID != nil && h.Services.Repo.CanWrite(r.Context(), repo, *userID)
	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.DiscussionDetail(view.DiscussionDetailData{
		BasePage:   h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "discussions", canManage),
		Repo:       *repo,
		Owner:      owner,
		RepoName:   repoName,
		Discussion: *discussion,
		Category:   category,
		Replies:    replies,
		BodyHTML:   markdown.Render(discussion.Body),
		CanWrite:   canWrite,
	}))
}

// CreateDiscussion handles POST /api/repos/{owner}/{repo}/discussions
func (h *Handler) CreateDiscussion(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	authRepo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !authRepo.AllowDiscussions {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), authRepo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var body struct {
		CategoryID int64  `json:"category_id"`
		Title      string `json:"title"`
		Body       string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	d, err := h.Services.Discussion.Create(r.Context(), owner, repoName, claims.UserID, claims.Username, body.CategoryID, body.Title, body.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// CreateReply handles POST /api/repos/{owner}/{repo}/discussions/{number}/replies
func (h *Handler) CreateReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}

	var body struct {
		Body     string `json:"body"`
		ParentID *int64 `json:"parent_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	reply, err := h.Services.Discussion.CreateReply(r.Context(), discussion.ID, claims.UserID, claims.Username, body.Body, body.ParentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		go h.Services.Notification.NotifyDiscussionReply(r.Context(), *repo, *discussion, claims.UserID, claims.Username)
	}

	writeJSON(w, http.StatusCreated, reply)
}

// MarkAnswer handles PATCH /api/repos/{owner}/{repo}/discussions/{number}
// Body: {"answer_id": 123} to mark, or {"answer_id": null} to clear.
func (h *Handler) MarkAnswer(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "write access required")
		return
	}

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}

	var body struct {
		AnswerID json.RawMessage `json:"answer_id"`
		Locked   *bool           `json:"locked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if body.AnswerID != nil {
		if string(body.AnswerID) == "null" {
			if err := h.Services.Discussion.SetAnswer(r.Context(), discussion.ID, nil); err != nil {
				slog.Error("operation failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
		} else {
			var replyID int64
			if err := json.Unmarshal(body.AnswerID, &replyID); err != nil {
				writeError(w, http.StatusBadRequest, "invalid answer_id")
				return
			}
			if err := h.Services.Discussion.SetAnswer(r.Context(), discussion.ID, &replyID); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if body.Locked != nil {
		if err := h.Services.Discussion.Lock(r.Context(), discussion.ID, *body.Locked); err != nil {
			slog.Error("operation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteDiscussionReply handles DELETE /api/repos/{owner}/{repo}/discussions/{number}/replies/{id}
func (h *Handler) DeleteDiscussionReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	numberStr := chi.URLParam(r, "number")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid discussion number")
		return
	}
	idStr := chi.URLParam(r, "id")
	replyID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reply id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "write access required")
		return
	}

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		writeError(w, http.StatusNotFound, "discussion not found")
		return
	}

	if err := h.Services.Discussion.DeleteReply(r.Context(), replyID, discussion.ID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateDiscussionCategory handles POST /api/repos/{owner}/{repo}/discussions/categories
func (h *Handler) CreateDiscussionCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "manage access required")
		return
	}

	var body struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	cat, err := h.Services.Discussion.CreateCategory(r.Context(), repo.ID, body.Name, body.Emoji)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, cat)
}

// DeleteDiscussionCategory handles DELETE /api/repos/{owner}/{repo}/discussions/categories/{id}
func (h *Handler) DeleteDiscussionCategory(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	idStr := chi.URLParam(r, "id")
	catID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "manage access required")
		return
	}

	if err := h.Services.Discussion.DeleteCategory(r.Context(), catID, repo.ID); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

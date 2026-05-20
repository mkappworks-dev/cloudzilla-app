package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
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

	categories, err := h.Services.Discussion.ListCategories(r.Context())
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

	labelsByDisc := make(map[int64][]model.Label, len(discussions))
	replyCounts := make(map[int64]int, len(discussions))
	participantsByDisc := make(map[int64][]string, len(discussions))
	for _, d := range discussions {
		ls, lerr := h.Services.Label.GetForDiscussion(r.Context(), d.ID)
		if lerr != nil {
			slog.Warn("discussions list: label fetch failed", "owner", owner, "repo", repoName, "discussion", d.ID, "error", lerr)
		} else if len(ls) > 0 {
			labelsByDisc[d.ID] = ls
		}
		replies, rerr := h.Services.Discussion.ListReplies(r.Context(), d.ID)
		if rerr != nil {
			slog.Warn("discussions list: reply fetch failed", "owner", owner, "repo", repoName, "discussion", d.ID, "error", rerr)
		}
		replyCounts[d.ID] = len(replies)
		seen := map[string]bool{d.AuthorName: true}
		parts := []string{d.AuthorName}
		for _, rp := range replies {
			if rp.AuthorName == "" || seen[rp.AuthorName] {
				continue
			}
			seen[rp.AuthorName] = true
			parts = append(parts, rp.AuthorName)
		}
		participantsByDisc[d.ID] = parts
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
		Labels:           labelsByDisc,
		ReplyCounts:      replyCounts,
		Participants:     participantsByDisc,
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

	rawReplies, repliesErr := h.Services.Discussion.ListReplies(r.Context(), discussion.ID)
	if repliesErr != nil {
		slog.Warn("discussion detail: reply fetch failed", "owner", owner, "repo", repoName, "discussion", discussion.ID, "error", repliesErr)
	}
	if rawReplies == nil {
		rawReplies = []model.DiscussionReply{}
	}
	var callerID int64
	if userID != nil {
		callerID = *userID
	}
	replies := make([]view.RenderedDiscussionReply, len(rawReplies))
	for i, rr := range rawReplies {
		rxn, rerr := h.Services.Reaction.ListByReply(r.Context(), rr.ID, callerID)
		if rerr != nil {
			slog.Warn("discussion detail: reply reactions failed", "discussion", discussion.ID, "reply", rr.ID, "error", rerr)
		}
		replies[i] = view.RenderedDiscussionReply{
			DiscussionReply: rr,
			BodyHTML:        markdown.Render(rr.Body),
			Reactions:       rxn,
		}
	}
	opReactions, opRxnErr := h.Services.Reaction.ListByDiscussion(r.Context(), discussion.ID, callerID)
	if opRxnErr != nil {
		slog.Warn("discussion detail: OP reactions failed", "discussion", discussion.ID, "error", opRxnErr)
	}

	participants := make([]string, 0, len(rawReplies)+1)
	seen := make(map[string]bool)
	addParticipant := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			participants = append(participants, name)
		}
	}
	addParticipant(discussion.AuthorName)
	for _, rr := range rawReplies {
		addParticipant(rr.AuthorName)
	}

	cats, catsErr := h.Services.Discussion.ListCategories(r.Context())
	if catsErr != nil {
		slog.Warn("discussion detail: category list failed", "owner", owner, "repo", repoName, "error", catsErr)
	}
	var category model.DiscussionCategory
	for _, c := range cats {
		if c.ID == discussion.CategoryID {
			category = c
			break
		}
	}

	labels, labelsErr := h.Services.Label.GetForDiscussion(r.Context(), discussion.ID)
	if labelsErr != nil {
		slog.Warn("discussion detail: label fetch failed", "owner", owner, "repo", repoName, "discussion", discussion.ID, "error", labelsErr)
	}
	if labels == nil {
		labels = []model.Label{}
	}
	allLabels, allLabelsErr := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if allLabelsErr != nil {
		slog.Warn("discussion detail: label list failed", "owner", owner, "repo", repoName, "error", allLabelsErr)
	}
	if allLabels == nil {
		allLabels = []model.Label{}
	}

	canWrite := userID != nil && h.Services.Repo.CanWrite(r.Context(), repo, *userID)
	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.DiscussionDetail(view.DiscussionDetailData{
		BasePage:      h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "discussions", canManage),
		Repo:          *repo,
		Owner:         owner,
		RepoName:      repoName,
		Discussion:    *discussion,
		Category:      category,
		AllCategories: cats,
		Labels:        labels,
		AllLabels:     allLabels,
		Replies:       replies,
		Participants:  participants,
		OPReactions:   opReactions,
		BodyHTML:      markdown.Render(discussion.Body),
		CanWrite:      canWrite,
	}))
}

func resolveDiscussionCategory(r *http.Request, categories []model.DiscussionCategory) int64 {
	if cidStr := r.URL.Query().Get("category"); cidStr != "" {
		if cid, err := strconv.ParseInt(cidStr, 10, 64); err == nil {
			for _, c := range categories {
				if c.ID == cid {
					return cid
				}
			}
		}
	}
	if len(categories) > 0 {
		return categories[0].ID
	}
	return 0
}

func (h *Handler) PageNewDiscussion(w http.ResponseWriter, r *http.Request) {
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

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	categories, err := h.Services.Discussion.ListCategories(r.Context())
	if err != nil {
		slog.Warn("new discussion: category list failed", "owner", owner, "repo", repoName, "error", err)
	}
	if categories == nil {
		categories = []model.DiscussionCategory{}
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)

	h.render(w, r, pages.DiscussionNew(view.DiscussionNewData{
		BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "discussions", canManage),
		Repo:             *repo,
		Owner:            owner,
		RepoName:         repoName,
		Categories:       categories,
		ActiveCategoryID: resolveDiscussionCategory(r, categories),
	}))
}

func (h *Handler) PageNewDiscussionSubmit(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	title := r.FormValue("title")
	body := r.FormValue("body")
	var categoryID int64
	if raw := r.FormValue("category_id"); raw != "" {
		v, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid category id")
			return
		}
		categoryID = v
	}

	categories, _ := h.Services.Discussion.ListCategories(r.Context())
	if categories == nil {
		categories = []model.DiscussionCategory{}
	}
	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	renderErr := func(msg string) {
		h.render(w, r, pages.DiscussionNew(view.DiscussionNewData{
			BasePage:         h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "discussions", canManage),
			Repo:             *repo,
			Owner:            owner,
			RepoName:         repoName,
			Categories:       categories,
			ActiveCategoryID: categoryID,
			Title:            title,
			Body:             body,
			Error:            msg,
		}))
	}

	if title == "" {
		renderErr("Title is required")
		return
	}
	if categoryID == 0 {
		renderErr("Pick a category for your discussion")
		return
	}

	d, err := h.Services.Discussion.Create(r.Context(), owner, repoName, claims.UserID, claims.Username, categoryID, title, body)
	if err != nil {
		renderErr("Failed to create discussion: " + err.Error())
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/discussions/"+strconv.Itoa(d.Number), http.StatusSeeOther)
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

	hxRequest := r.Header.Get("HX-Request") == "true"

	var replyBody string
	var parentID *int64
	if hxRequest {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		replyBody = r.FormValue("body")
		if pid := r.FormValue("parent_id"); pid != "" {
			if v, err := strconv.ParseInt(pid, 10, 64); err == nil {
				parentID = &v
			}
		}
	} else {
		var body struct {
			Body     string `json:"body"`
			ParentID *int64 `json:"parent_id,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		replyBody, parentID = body.Body, body.ParentID
	}

	reply, err := h.Services.Discussion.CreateReply(r.Context(), discussion.ID, claims.UserID, claims.Username, replyBody, parentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	repo, repoErr := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repoErr != nil {
		// The reply already landed; we just can't fan out notifications or
		// render the post-reply card with full write affordances.
		slog.Warn("create reply: post-reply repo fetch failed", "owner", owner, "repo", repoName, "discussion", discussion.ID, "error", repoErr)
	}
	if repo != nil {
		go h.Services.Notification.NotifyDiscussionReply(r.Context(), *repo, *discussion, claims.UserID, claims.Username)
	}

	if hxRequest {
		canWrite := repo != nil && h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		allReplies, _ := h.Services.Discussion.ListReplies(r.Context(), discussion.ID)
		rendered := view.RenderedDiscussionReply{
			DiscussionReply: *reply,
			BodyHTML:        markdown.Render(reply.Body),
		}
		h.render(w, r, pages.DiscussionReplyCreated(owner, repoName, number, rendered, len(allReplies), canWrite, true))
		return
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
		AnswerID   json.RawMessage `json:"answer_id"`
		Locked     *bool           `json:"locked"`
		Title      *string         `json:"title"`
		Body       *string         `json:"body"`
		CategoryID *int64          `json:"category_id"`
	}
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		if r.Form.Has("answer_id") {
			// hx-vals serializes JSON null to the literal string "null".
			v := r.FormValue("answer_id")
			if v == "" {
				v = "null"
			}
			body.AnswerID = json.RawMessage(v)
		}
		if r.Form.Has("locked") {
			locked := r.FormValue("locked") == "true"
			body.Locked = &locked
		}
		if r.Form.Has("title") {
			title := r.FormValue("title")
			body.Title = &title
		}
		if r.Form.Has("body") {
			text := r.FormValue("body")
			body.Body = &text
		}
		if r.Form.Has("category_id") {
			v, err := strconv.ParseInt(r.FormValue("category_id"), 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid category id")
				return
			}
			body.CategoryID = &v
		}
	} else if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}
		// Length cap is enforced by DiscussionService.UpdateContent.
		body.Title = &title
	}
	if body.CategoryID != nil {
		cat, cerr := h.Services.Discussion.GetCategory(r.Context(), *body.CategoryID)
		if cerr != nil {
			slog.Error("mark answer: category lookup failed", "discussion", discussion.ID, "category", *body.CategoryID, "error", cerr)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if cat == nil {
			writeError(w, http.StatusUnprocessableEntity, "unknown category")
			return
		}
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
	if body.Title != nil || body.Body != nil {
		title := discussion.Title
		if body.Title != nil {
			title = *body.Title
		}
		bodyText := discussion.Body
		if body.Body != nil {
			bodyText = *body.Body
		}
		if err := h.Services.Discussion.UpdateContent(r.Context(), discussion.ID, title, bodyText); err != nil {
			if errors.Is(err, service.ErrTitleTooLong) {
				writeError(w, http.StatusBadRequest, "title is too long")
				return
			}
			slog.Error("mark answer: update content failed", "discussion", discussion.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}
	if body.CategoryID != nil {
		if err := h.Services.Discussion.SetCategory(r.Context(), discussion.ID, *body.CategoryID); err != nil {
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

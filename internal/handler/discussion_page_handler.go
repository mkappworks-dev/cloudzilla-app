package handler

import (
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

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	if !repo.AllowDiscussions {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	userID := viewerOf(r)

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

	var avatarNames []string
	for _, parts := range participantsByDisc {
		avatarNames = append(avatarNames, parts...)
	}
	h.render(w, h.withAvatars(r, avatarNames...), pages.Discussions(view.DiscussionsData{
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

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}
	if !repo.AllowDiscussions {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	userID := viewerOf(r)

	discussion, err := h.Services.Discussion.Get(r.Context(), owner, repoName, number)
	if err != nil || discussion == nil {
		http.Error(w, "discussion not found", http.StatusNotFound)
		return
	}

	// The body renders before the replies so it is first in line for the request's highlight budget.
	bodyHTML := markdown.RenderCtx(r.Context(), discussion.Body)
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
			BodyHTML:        markdown.RenderCtx(r.Context(), rr.Body),
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

	threadSub, err := h.threadSubscriptionData(r, repo, model.ThreadKindDiscussion, "discussions", discussion.Number)
	if err != nil {
		slog.Warn("discussion detail: subscription lookup failed; rendering as not subscribed", "owner", owner, "repo", repoName, "discussion", discussion.Number, "error", err)
	}

	h.render(w, h.withAvatars(r, participants...), pages.DiscussionDetail(view.DiscussionDetailData{
		BasePage:           h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "discussions", canManage),
		Repo:               *repo,
		Owner:              owner,
		RepoName:           repoName,
		Discussion:         *discussion,
		Category:           category,
		AllCategories:      cats,
		Labels:             labels,
		AllLabels:          allLabels,
		Replies:            replies,
		Participants:       participants,
		OPReactions:        opReactions,
		BodyHTML:           bodyHTML,
		CanWrite:           canWrite,
		ThreadSubscription: threadSub,
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

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	if !repo.AllowDiscussions {
		h.NotFound(w, r)
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

	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	if !repo.AllowDiscussions {
		h.NotFound(w, r)
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
		renderErr(createFailedMessage(err, "discussion", "owner", owner, "repo", repoName))
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/discussions/"+strconv.Itoa(d.Number), http.StatusSeeOther)
}

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// releaseStatus collapses the release flags into a single keyword the title
// fragment uses to pick a badge: "latest" / "prerelease" / "draft" / "".
func (h *Handler) releaseStatus(ctx context.Context, owner, repoName string, release model.Release) string {
	if !release.IsDraft && !release.IsPrerelease {
		if latest, err := h.Services.Release.GetLatest(ctx, owner, repoName); err == nil && latest != nil && latest.ID == release.ID {
			return "latest"
		}
	}
	switch {
	case release.IsPrerelease:
		return "prerelease"
	case release.IsDraft:
		return "draft"
	}
	return ""
}

// releaseWriteContext loads the release and resolves caller permissions for
// inline-edit handlers. Returns ok=false after writing the response on failure.
func (h *Handler) releaseWriteContext(w http.ResponseWriter, r *http.Request) (owner, repoName string, release *model.Release, canWrite bool, ok bool) {
	owner = chi.URLParam(r, "owner")
	repoName = chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	claims, found := middleware.ClaimsFromContext(r.Context())
	if !found {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	if !canWrite {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	release, err = h.Services.Release.GetByID(r.Context(), owner, repoName, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	ok = true
	return
}

// ReleaseTitleSection handles GET /api/repos/{owner}/{repo}/releases/{id}/title
// (?mode=edit renders the inline edit form).
func (h *Handler) ReleaseTitleSection(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	var userID *int64
	canWrite := false
	if claims, found := middleware.ClaimsFromContext(r.Context()); found {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	release, err := h.Services.Release.GetByID(r.Context(), owner, repoName, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	status := h.releaseStatus(r.Context(), owner, repoName, *release)
	editing := r.URL.Query().Get("mode") == "edit"
	h.render(w, r, fragments.ReleaseTitleSection(owner, repoName, release.ID, release.Name, release.TagName, status, canWrite, editing))
}

// EditReleaseTitle handles PATCH /api/repos/{owner}/{repo}/releases/{id}/title.
func (h *Handler) EditReleaseTitle(w http.ResponseWriter, r *http.Request) {
	owner, repoName, release, _, ok := h.releaseWriteContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.Services.Release.EditName(r.Context(), owner, repoName, release.ID, r.FormValue("name"))
	if err != nil {
		slog.Error("edit release title failed", "owner", owner, "repo", repoName, "id", release.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	status := h.releaseStatus(r.Context(), owner, repoName, *updated)
	h.render(w, r, fragments.ReleaseTitleSection(owner, repoName, updated.ID, updated.Name, updated.TagName, status, true, false))
}

// ReleaseBodySection handles GET /api/repos/{owner}/{repo}/releases/{id}/body
// (?mode=edit renders the inline edit form).
func (h *Handler) ReleaseBodySection(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	var userID *int64
	canWrite := false
	if claims, found := middleware.ClaimsFromContext(r.Context()); found {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		userID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	release, err := h.Services.Release.GetByID(r.Context(), owner, repoName, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	h.render(w, r, fragments.ReleaseBodyCard(view.ReleaseBodyCardData{
		Owner:     owner,
		RepoName:  repoName,
		ReleaseID: release.ID,
		Body:      release.Body,
		BodyHTML:  markdown.Render(release.Body),
		CanWrite:  canWrite,
		Editing:   r.URL.Query().Get("mode") == "edit",
	}))
}

// PublishRelease handles PATCH /api/repos/{owner}/{repo}/releases/{id}/publish.
// Draft → published is one-way; ErrReleaseAlreadyPublished surfaces as 422.
// Fires the same release webhook + audit event as a fresh non-draft create.
func (h *Handler) PublishRelease(w http.ResponseWriter, r *http.Request) {
	owner, repoName, release, _, ok := h.releaseWriteContext(w, r)
	if !ok {
		return
	}
	claims, _ := middleware.ClaimsFromContext(r.Context())
	updated, err := h.Services.Release.Publish(r.Context(), owner, repoName, release.ID)
	if err != nil {
		if errors.Is(err, service.ErrReleaseAlreadyPublished) {
			msg := "This release is already published."
			if r.Header.Get("HX-Request") == "true" {
				toast(w, "error", msg)
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			writeError(w, http.StatusUnprocessableEntity, msg)
			return
		}
		slog.Error("publish release failed", "owner", owner, "repo", repoName, "id", release.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	go h.Services.Webhook.Dispatch(updated.RepoID, "release", h.Services.Webhook.ReleasePayload("published", owner, repoName, *updated))
	repoID := updated.RepoID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventReleasePublished, map[string]any{"tag": updated.TagName, "name": updated.Name})

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// EditReleasePrerelease handles PATCH /api/repos/{owner}/{repo}/releases/{id}/prerelease.
// The HTMX response asks the page to refresh so the title badge, sidebar status,
// and any other derived state re-render server-side from one source of truth.
func (h *Handler) EditReleasePrerelease(w http.ResponseWriter, r *http.Request) {
	owner, repoName, release, _, ok := h.releaseWriteContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	isPrerelease := r.FormValue("is_prerelease") == "true"
	if _, err := h.Services.Release.EditPrerelease(r.Context(), owner, repoName, release.ID, isPrerelease); err != nil {
		slog.Error("edit release prerelease failed", "owner", owner, "repo", repoName, "id", release.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EditReleaseBody handles PATCH /api/repos/{owner}/{repo}/releases/{id}/body.
func (h *Handler) EditReleaseBody(w http.ResponseWriter, r *http.Request) {
	owner, repoName, release, _, ok := h.releaseWriteContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.Services.Release.EditBody(r.Context(), owner, repoName, release.ID, r.FormValue("body"))
	if err != nil {
		slog.Error("edit release body failed", "owner", owner, "repo", repoName, "id", release.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.render(w, r, fragments.ReleaseBodyCard(view.ReleaseBodyCardData{
		Owner:     owner,
		RepoName:  repoName,
		ReleaseID: updated.ID,
		Body:      updated.Body,
		BodyHTML:  markdown.Render(updated.Body),
		CanWrite:  true,
		Editing:   false,
	}))
}

func (h *Handler) PageReleases(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		userID = &claims.UserID
	}
	_ = userID

	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
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

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
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
	tagName := chi.URLParam(r, "tagName")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	var userID *int64
	canWrite := false
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		userID = &claims.UserID
	}

	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
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

	h.render(w, r, pages.ReleaseDetail(view.ReleaseDetailData{
		BasePage:   h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "releases", canManage),
		Repo:       *repo,
		Owner:      owner,
		RepoName:   repoName,
		Release:    *release,
		BodyHTML:   markdown.Render(release.Body),
		CanWrite:   canWrite,
		AuthorName: authorName,
		IsLatest:   isLatest,
		CommitSHA:  commitSHA,
	}))
}

func (h *Handler) ListReleases(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	if !h.viewerCanReadRepo(r, owner, repoName) {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	releases, err := h.Services.Release.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if releases == nil {
		releases = []model.Release{}
	}
	writeJSON(w, http.StatusOK, releases)
}

func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}

	if !h.viewerCanReadRepo(r, owner, repoName) {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}

	release, err := h.Services.Release.GetByID(r.Context(), owner, repoName, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	writeJSON(w, http.StatusOK, release)
}

func (h *Handler) GetLatestRelease(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	if !h.viewerCanReadRepo(r, owner, repoName) {
		writeError(w, http.StatusNotFound, "no release found")
		return
	}

	release, err := h.Services.Release.GetLatest(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "no release found")
		return
	}
	writeJSON(w, http.StatusOK, release)
}

type createReleaseRequest struct {
	TagName      string `json:"tag_name"`
	Target       string `json:"target"`
	Name         string `json:"name"`
	Body         string `json:"body"`
	IsPrerelease bool   `json:"is_prerelease"`
	IsDraft      bool   `json:"is_draft"`
}

func (h *Handler) CreateRelease(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var tagName, target, name, body string
	var isPrerelease, isDraft bool

	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		tagName = r.FormValue("tag_name")
		target = r.FormValue("target")
		name = r.FormValue("name")
		body = r.FormValue("body")
		isPrerelease = r.FormValue("is_prerelease") == "true"
		isDraft = r.FormValue("is_draft") == "true"
	} else {
		var req createReleaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		tagName, target, name, body = req.TagName, req.Target, req.Name, req.Body
		isPrerelease, isDraft = req.IsPrerelease, req.IsDraft
	}

	release, err := h.Services.Release.Create(r.Context(), owner, repoName, tagName, target, name, body, isPrerelease, isDraft, claims.UserID)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, service.ErrReleaseTagInUse) {
			msg = "A release for tag " + tagName + " already exists."
		}
		if r.Header.Get("HX-Request") == "true" {
			toast(w, "error", msg)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}

	go h.Services.Webhook.Dispatch(release.RepoID, "release", h.Services.Webhook.ReleasePayload("published", owner, repoName, *release))
	repoID := release.RepoID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventReleasePublished, map[string]any{"tag": release.TagName, "name": release.Name})

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/"+owner+"/"+repoName+"/releases")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusCreated, release)
}

type updateReleaseRequest struct {
	TagName      string `json:"tag_name"`
	Name         string `json:"name"`
	Body         string `json:"body"`
	IsPrerelease bool   `json:"is_prerelease"`
	IsDraft      bool   `json:"is_draft"`
}

func (h *Handler) UpdateRelease(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var tagName, name, body string
	var isPrerelease, isDraft bool

	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		tagName = r.FormValue("tag_name")
		name = r.FormValue("name")
		body = r.FormValue("body")
		isPrerelease = r.FormValue("is_prerelease") == "true"
		isDraft = r.FormValue("is_draft") == "true"
	} else {
		var req updateReleaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		tagName, name, body = req.TagName, req.Name, req.Body
		isPrerelease, isDraft = req.IsPrerelease, req.IsDraft
	}

	release, err := h.Services.Release.Update(r.Context(), owner, repoName, id, tagName, name, body, isPrerelease, isDraft)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/"+owner+"/"+repoName+"/releases/tag/"+release.TagName)
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, release)
}

func (h *Handler) DeleteRelease(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid release id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.Services.Release.Delete(r.Context(), owner, repoName, id); err != nil {
		slog.Error("operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/"+owner+"/"+repoName+"/releases")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

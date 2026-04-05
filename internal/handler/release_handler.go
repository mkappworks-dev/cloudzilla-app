package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

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
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		userID = &claims.UserID
	}
	_ = userID

	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	releases, _ := h.Services.Release.ListByRepo(r.Context(), owner, repoName)
	if releases == nil {
		releases = []model.Release{}
	}

	h.render(w, r, pages.Releases(view.ReleasesData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Releases: releases,
		CanWrite: canWrite,
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
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
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

	h.render(w, r, pages.ReleaseDetail(view.ReleaseDetailData{
		BasePage: basePage(r, h.Services),
		Repo:     *repo,
		Owner:    owner,
		RepoName: repoName,
		Release:  *release,
		BodyHTML: markdown.Render(release.Body),
		CanWrite: canWrite,
	}))
}

func (h *Handler) ListReleases(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

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
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

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

	release, err := h.Services.Release.GetLatest(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "no release found")
		return
	}
	writeJSON(w, http.StatusOK, release)
}

type createReleaseRequest struct {
	TagName      string `json:"tag_name"`
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
		var req createReleaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		tagName, name, body = req.TagName, req.Name, req.Body
		isPrerelease, isDraft = req.IsPrerelease, req.IsDraft
	}

	release, err := h.Services.Release.Create(r.Context(), owner, repoName, tagName, name, body, isPrerelease, isDraft, claims.UserID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/"+owner+"/"+repoName+"/releases")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

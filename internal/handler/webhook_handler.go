package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

type createWebhookRequest struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
	Events string `json:"events"`
}

func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	hooks, err := h.Services.Webhook.ListByRepo(r.Context(), repo.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}
	if hooks == nil {
		hooks = []model.Webhook{}
	}
	writeJSON(w, http.StatusOK, hooks)
}

func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
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

	var url, secret, events string
	if r.Header.Get("HX-Request") == "true" {
		r.ParseForm()
		url = r.FormValue("url")
		secret = r.FormValue("secret")
		events = r.FormValue("events")
	} else {
		var req createWebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		url = req.URL
		secret = req.Secret
		events = req.Events
	}

	wh, err := h.Services.Webhook.Create(r.Context(), repo.ID, url, secret, events)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		hooks, _ := h.Services.Webhook.ListByRepo(r.Context(), repo.ID)
		if hooks == nil {
			hooks = []model.Webhook{}
		}
		h.render(w, r, fragments.WebhooksList(view.WebhooksFragData{
			Owner:    owner,
			RepoName: repoName,
			RepoID:   repo.ID,
			Webhooks: hooks,
			CanWrite: true,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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

	if err := h.Services.Webhook.Delete(r.Context(), id, repo.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete webhook")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		hooks, _ := h.Services.Webhook.ListByRepo(r.Context(), repo.ID)
		if hooks == nil {
			hooks = []model.Webhook{}
		}
		h.render(w, r, fragments.WebhooksList(view.WebhooksFragData{
			Owner:    owner,
			RepoName: repoName,
			RepoID:   repo.ID,
			Webhooks: hooks,
			CanWrite: true,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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

	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	deliveries, err := h.Services.Webhook.ListDeliveries(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}
	writeJSON(w, http.StatusOK, deliveries)
}

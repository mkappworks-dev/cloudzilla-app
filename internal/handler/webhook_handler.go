package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
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

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var url, secret, events string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}
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

	if url == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
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
			CanManage: true,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
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

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
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
			CanManage: true,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
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
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	deliveries, err := h.Services.Webhook.ListDeliveries(r.Context(), id, repo.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}
	if deliveries == nil {
		deliveries = []model.WebhookDelivery{}
	}

	if r.Header.Get("HX-Request") == "true" {
		canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		h.render(w, r, fragments.WebhookDeliveriesList(view.WebhookDeliveriesFragData{
			Owner:      owner,
			RepoName:   repoName,
			WebhookID:  id,
			Deliveries: deliveries,
			CanManage:  canManage,
		}))
		return
	}
	writeJSON(w, http.StatusOK, deliveries)
}

// UpdateWebhook handles PATCH /api/repos/{owner}/{repo}/hooks/{id} — updates event filter.
func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	hookID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid hook id")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var events string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}
		var parts []string
		for _, ev := range []string{"push", "issues", "pull_request"} {
			if r.FormValue(ev) == "1" {
				parts = append(parts, ev)
			}
		}
		if len(parts) == 0 {
			writeError(w, http.StatusBadRequest, "at least one event must be selected")
			return
		}
		events = strings.Join(parts, ",")
	} else {
		var req struct {
			Events string `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		allowed := map[string]bool{"push": true, "issues": true, "pull_request": true}
		for _, ev := range strings.Split(req.Events, ",") {
			ev = strings.TrimSpace(ev)
			if ev != "" && !allowed[ev] {
				writeError(w, http.StatusBadRequest, "invalid event: "+ev)
				return
			}
		}
		events = req.Events
	}

	if err := h.Services.Webhook.UpdateEvents(r.Context(), hookID, repo.ID, events); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
			CanManage: true,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RedeliverWebhook handles POST /api/repos/{owner}/{repo}/hooks/{id}/redeliver
func (h *Handler) RedeliverWebhook(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	deliveryIDStr := r.URL.Query().Get("delivery_id")
	deliveryID, err := strconv.ParseInt(deliveryIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "delivery_id required")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.Services.Webhook.RedeliverByID(r.Context(), deliveryID, repo.ID); err != nil {
		if err.Error() == "forbidden" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

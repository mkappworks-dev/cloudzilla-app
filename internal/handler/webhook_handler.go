package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

const webhookFormError = "#webhook-form-error"

type createWebhookRequest struct {
	URL       string `json:"url"`
	Secret    string `json:"secret"`
	Events    string `json:"events"`
	Password  string `json:"password"`
	Code      string `json:"code"`
	EmailCode string `json:"email_code"`
}

// webhookInput is a create-webhook request, whether the JSON API's or the settings dialog's HTMX form.
type webhookInput struct {
	url, secret, events string
	confirm             service.Confirmation
	htmx                bool
}

func readWebhookInput(w http.ResponseWriter, r *http.Request) (webhookInput, bool) {
	in := webhookInput{htmx: r.Header.Get("HX-Request") == "true"}
	if in.htmx {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return in, false
		}
		in.url, in.secret, in.events = r.FormValue("url"), r.FormValue("secret"), r.FormValue("events")
		in.confirm = confirmationFrom(r)
		return in, true
	}
	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return in, false
	}
	in.url, in.secret, in.events = req.URL, req.Secret, req.Events
	in.confirm = service.Confirmation{Password: req.Password, Code: req.Code, OneTimeCode: req.EmailCode}
	return in, true
}

// webhookEvents are the events a hook can subscribe to; a new hook gets the first three.
var webhookEvents = []string{"push", "issues", "pull_request", "workflow_run"}

// readWebhookEvents reads the event filter of an update request, answering 400 itself when it is invalid.
func readWebhookEvents(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return "", false
		}
		var parts []string
		for _, ev := range webhookEvents {
			if r.FormValue(ev) == "1" {
				parts = append(parts, ev)
			}
		}
		if len(parts) == 0 {
			writeError(w, http.StatusBadRequest, "at least one event must be selected")
			return "", false
		}
		return strings.Join(parts, ","), true
	}
	var req struct {
		Events string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	for _, ev := range strings.Split(req.Events, ",") {
		ev = strings.TrimSpace(ev)
		if ev != "" && !slices.Contains(webhookEvents, ev) {
			writeError(w, http.StatusBadRequest, "invalid event: "+ev)
			return "", false
		}
	}
	return req.Events, true
}

func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
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

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	in, ok := readWebhookInput(w, r)
	if !ok {
		return
	}
	url, secret, events, confirm, htmx := in.url, in.secret, in.events, in.confirm, in.htmx

	if url == "" {
		if htmx {
			renderFormError(w, webhookFormError, "Payload URL is required.")
			return
		}
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if !h.confirmGrant(w, r, claims.UserID, confirm, webhookFormError) {
		return
	}

	wh, err := h.Services.Webhook.Create(r.Context(), repo.ID, url, secret, events)
	var urlErr *service.WebhookURLError
	if errors.As(err, &urlErr) {
		if htmx {
			renderFormError(w, webhookFormError, urlErr.Reason)
			return
		}
		writeError(w, http.StatusBadRequest, urlErr.Reason)
		return
	}
	if err != nil {
		slog.Error("operation failed", "error", err)
		if htmx {
			renderFormError(w, webhookFormError, "Couldn't add the webhook. Please try again.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if htmx {
		hooks, _ := h.Services.Webhook.ListByRepo(r.Context(), repo.ID)
		if hooks == nil {
			hooks = []model.Webhook{}
		}
		h.render(w, r, fragments.WebhooksList(view.WebhooksFragData{
			Owner:     owner,
			RepoName:  repoName,
			RepoID:    repo.ID,
			Webhooks:  hooks,
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

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
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
			Owner:     owner,
			RepoName:  repoName,
			RepoID:    repo.ID,
			Webhooks:  hooks,
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
	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	deliveries, err := h.Services.Webhook.ListDeliveries(r.Context(), id, repo.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}
	if deliveries == nil {
		deliveries = []model.WebhookDelivery{}
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.WebhookDeliveriesList(view.WebhookDeliveriesFragData{
			Owner:      owner,
			RepoName:   repoName,
			WebhookID:  id,
			Deliveries: deliveries,
			CanManage:  true, // already verified above
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

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}

	events, ok := readWebhookEvents(w, r)
	if !ok {
		return
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
			Owner:     owner,
			RepoName:  repoName,
			RepoID:    repo.ID,
			Webhooks:  hooks,
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
	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	deliveryID, err := strconv.ParseInt(r.URL.Query().Get("delivery_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "delivery_id required")
		return
	}

	if err := h.Services.Webhook.RedeliverByID(r.Context(), deliveryID, repo.ID); err != nil {
		if err.Error() == "forbidden" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "delivery not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// ownedOrgJSON loads the {org} the caller owns, answering 401, 404 or 403 itself otherwise. Webhooks carry
// secrets, so a member, or a repository admin, doesn't pass.
func (h *Handler) ownedOrgJSON(w http.ResponseWriter, r *http.Request) (*model.Organization, int64, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, 0, false
	}
	org, err := h.Services.Org.Get(r.Context(), chi.URLParam(r, "org"))
	if err != nil {
		writeError(w, http.StatusNotFound, "org not found")
		return nil, 0, false
	}
	if !h.Services.Org.IsOwner(r.Context(), org.ID, claims.UserID) {
		writeError(w, http.StatusForbidden, "only org owners can do this")
		return nil, 0, false
	}
	return org, claims.UserID, true
}

func (h *Handler) renderOrgWebhooks(w http.ResponseWriter, r *http.Request, org *model.Organization) {
	hooks, _ := h.Services.Webhook.ListByOrg(r.Context(), org.ID)
	if hooks == nil {
		hooks = []model.Webhook{}
	}
	h.render(w, r, fragments.WebhooksList(view.WebhooksFragData{Owner: org.Name, Webhooks: hooks, CanManage: true}))
}

func (h *Handler) ListOrgWebhooks(w http.ResponseWriter, r *http.Request) {
	org, _, ok := h.ownedOrgJSON(w, r)
	if !ok {
		return
	}
	hooks, err := h.Services.Webhook.ListByOrg(r.Context(), org.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}
	if hooks == nil {
		hooks = []model.Webhook{}
	}
	writeJSON(w, http.StatusOK, hooks)
}

func (h *Handler) CreateOrgWebhook(w http.ResponseWriter, r *http.Request) {
	org, userID, ok := h.ownedOrgJSON(w, r)
	if !ok {
		return
	}
	in, ok := readWebhookInput(w, r)
	if !ok {
		return
	}
	if in.url == "" {
		if in.htmx {
			renderFormError(w, webhookFormError, "Payload URL is required.")
			return
		}
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if !h.confirmGrant(w, r, userID, in.confirm, webhookFormError) {
		return
	}

	wh, err := h.Services.Webhook.CreateForOrg(r.Context(), org.ID, in.url, in.secret, in.events)
	var urlErr *service.WebhookURLError
	if errors.As(err, &urlErr) {
		if in.htmx {
			renderFormError(w, webhookFormError, urlErr.Reason)
			return
		}
		writeError(w, http.StatusBadRequest, urlErr.Reason)
		return
	}
	if err != nil {
		slog.Error("operation failed", "error", err)
		if in.htmx {
			renderFormError(w, webhookFormError, "Couldn't add the webhook. Please try again.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if in.htmx {
		h.renderOrgWebhooks(w, r, org)
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

func (h *Handler) DeleteOrgWebhook(w http.ResponseWriter, r *http.Request) {
	org, _, ok := h.ownedOrgJSON(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	if err := h.Services.Webhook.DeleteForOrg(r.Context(), id, org.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete webhook")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderOrgWebhooks(w, r, org)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListOrgWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	org, _, ok := h.ownedOrgJSON(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	deliveries, err := h.Services.Webhook.ListDeliveriesForOrg(r.Context(), id, org.ID)
	if err != nil {
		switch {
		case err.Error() == "forbidden":
			writeError(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "webhook not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		}
		return
	}
	if deliveries == nil {
		deliveries = []model.WebhookDelivery{}
	}
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.WebhookDeliveriesList(view.WebhookDeliveriesFragData{
			Owner:      org.Name,
			WebhookID:  id,
			Deliveries: deliveries,
			CanManage:  true,
		}))
		return
	}
	writeJSON(w, http.StatusOK, deliveries)
}

// UpdateOrgWebhook handles PATCH /api/orgs/{org}/hooks/{id} — updates the event filter.
func (h *Handler) UpdateOrgWebhook(w http.ResponseWriter, r *http.Request) {
	org, _, ok := h.ownedOrgJSON(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid hook id")
		return
	}
	events, ok := readWebhookEvents(w, r)
	if !ok {
		return
	}
	if err := h.Services.Webhook.UpdateEventsForOrg(r.Context(), id, org.ID, events); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderOrgWebhooks(w, r, org)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RedeliverOrgWebhook handles POST /api/orgs/{org}/hooks/{id}/redeliver.
func (h *Handler) RedeliverOrgWebhook(w http.ResponseWriter, r *http.Request) {
	org, _, ok := h.ownedOrgJSON(w, r)
	if !ok {
		return
	}
	deliveryID, err := strconv.ParseInt(r.URL.Query().Get("delivery_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "delivery_id required")
		return
	}
	if err := h.Services.Webhook.RedeliverForOrg(r.Context(), deliveryID, org.ID); err != nil {
		switch {
		case err.Error() == "forbidden":
			writeError(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "delivery not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// CloseSecretAlert closes an open alert with the resolution the form names.
func (h *Handler) CloseSecretAlert(w http.ResponseWriter, r *http.Request) {
	h.secretAlertAction(w, r, model.AuditActionSecretScanClose, func(r *http.Request, alert *model.SecretScanAlert, actorID int64) (string, error) {
		resolution := r.FormValue("resolution")
		return resolution, h.Services.SecretAlert.Close(r.Context(), alert.ID, actorID, resolution)
	})
}

// ReopenSecretAlert reopens a closed alert, so its value is blocked again.
func (h *Handler) ReopenSecretAlert(w http.ResponseWriter, r *http.Request) {
	h.secretAlertAction(w, r, model.AuditActionSecretScanReopen, func(r *http.Request, alert *model.SecretScanAlert, actorID int64) (string, error) {
		return "", h.Services.SecretAlert.Reopen(r.Context(), alert.ID, actorID)
	})
}

// AllowSecretAlert closes an alert as a false positive or test value, which stops its value being blocked.
func (h *Handler) AllowSecretAlert(w http.ResponseWriter, r *http.Request) {
	h.secretAlertAction(w, r, model.AuditActionSecretScanAllow, func(r *http.Request, alert *model.SecretScanAlert, actorID int64) (string, error) {
		resolution := r.FormValue("resolution")
		return resolution, h.Services.SecretAlert.Allow(r.Context(), alert.ID, actorID, resolution)
	})
}

// secretAlertAction runs one alert transition for a writer and, when it succeeds, records the audit entry and goes
// back to the alert page. apply returns the resolution to put in the audit metadata.
func (h *Handler) secretAlertAction(w http.ResponseWriter, r *http.Request, auditAction string,
	apply func(*http.Request, *model.SecretScanAlert, int64) (string, error),
) {
	repo, actorID, ok := h.securityRepo(w, r)
	if !ok {
		return
	}
	alert, ok := h.securityAlert(w, r, repo)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		settingsError(w, r, http.StatusBadRequest, "invalid form data")
		return
	}

	resolution, err := apply(r, alert, actorID)
	switch {
	case errors.Is(err, service.ErrSecretResolution):
		settingsError(w, r, http.StatusBadRequest, "choose a resolution: revoked, false positive or used in tests")
		return
	case errors.Is(err, service.ErrSecretAllowRefused):
		settingsError(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, service.ErrSecretAlertState):
		settingsError(w, r, http.StatusConflict, "the alert is not in a state that allows this; reload the page")
		return
	case errors.Is(err, service.ErrSecretAlertNotFound):
		h.NotFound(w, r)
		return
	case err != nil:
		slog.Error("security: alert action failed", "action", auditAction, "alert_id", alert.ID, "error", err)
		h.ServerError(w, r)
		return
	}

	meta := map[string]any{"repo_id": repo.ID, "number": alert.Number, "secret_type": alert.SecretType}
	if resolution != "" {
		meta["resolution"] = resolution
	}
	claims, _ := middleware.ClaimsFromContext(r.Context())
	h.Services.AuditLog.Record(r.Context(), r, actorID, claims.Username, auditAction, model.AuditTargetSecretAlert, alert.ID,
		repo.OwnerName+"/"+repo.Name+"#"+strconv.Itoa(alert.Number), meta)

	redirectAfterSave(w, r, "/"+repo.OwnerName+"/"+repo.Name+"/security/secrets/"+strconv.Itoa(alert.Number))
}

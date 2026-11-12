package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// RequestExport handles POST /settings/export.
func (h *Handler) RequestExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := h.Services.Export.Request(r.Context(), claims.UserID); err != nil {
		code := "export_failed"
		switch {
		case errors.Is(err, service.ErrExportInProgress):
			code = "export_in_progress"
		case errors.Is(err, service.ErrExportTooSoon):
			code = "export_too_soon"
		case errors.Is(err, service.ErrStorageUnconfigured):
			code = "export_unavailable"
		default:
			slog.Error("request export", "user_id", claims.UserID, "error", err)
		}
		http.Redirect(w, r, "/settings?profile_error="+code+"#export", http.StatusSeeOther)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionExportRequest, model.AuditTargetUser, claims.UserID, claims.Username, nil)
	http.Redirect(w, r, "/settings#export", http.StatusSeeOther)
}

// DownloadExport handles POST /settings/export/{id}/download. It is a POST so the confirmation travels in the body.
func (h *Handler) DownloadExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		redirectReauthRefusal(w, r, claims.UserID, err, "", "export")
		return
	}
	body, err := h.Services.Export.Open(r.Context(), claims.UserID, id)
	if errors.Is(err, service.ErrExportNotFound) {
		http.Redirect(w, r, "/settings?profile_error=export_gone#export", http.StatusSeeOther)
		return
	}
	if err != nil {
		slog.Error("open export", "user_id", claims.UserID, "export_id", id, "error", err)
		http.Error(w, "storage unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = body.Close() }()
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionExportDownload, model.AuditTargetUser, claims.UserID, claims.Username, nil)
	extendDeadlines(w)
	hd := w.Header()
	hd.Set("Content-Type", "application/zip")
	hd.Set("Content-Disposition", `attachment; filename="cloudzilla-export-`+strconv.FormatInt(id, 10)+`.zip"`)
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("stream export", "export_id", id, "error", err)
	}
}

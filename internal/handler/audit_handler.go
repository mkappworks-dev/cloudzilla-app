package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// PageAuditLog serves GET /admin/audit-log
func (h *Handler) PageAuditLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	const perPage = 50

	filter := model.AuditFilter{
		Action:     r.URL.Query().Get("action"),
		TargetType: r.URL.Query().Get("target_type"),
	}

	entries, total, err := h.Services.AuditLog.List(ctx, filter, page, perPage)
	if err != nil {
		slog.Error("failed to list audit log entries", "error", err)
		http.Error(w, "Failed to load audit log", http.StatusInternalServerError)
		return
	}

	data := view.AuditLogData{
		BasePage:   basePage(r, h.Services),
		Entries:    entries,
		Filter:     filter,
		TotalCount: total,
		Page:       page,
		PerPage:    perPage,
	}
	h.render(w, r, pages.AuditLog(data))
}

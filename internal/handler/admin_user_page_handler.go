package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const adminUsersPerPage = 50

// maxAdminUsersPage keeps the offset from overflowing into a negative one, which Postgres rejects.
const maxAdminUsersPage = 1 << 20

// PageAdminUsers serves GET /admin/users.
func (h *Handler) PageAdminUsers(w http.ResponseWriter, r *http.Request) {
	page := 1
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 0 {
		page = min(n, maxAdminUsersPage)
	}
	f := model.AdminUserFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Role:   r.URL.Query().Get("role"),
		Status: r.URL.Query().Get("status"),
	}
	users, total, err := h.Services.AdminUser.List(r.Context(), f, page, adminUsersPerPage)
	if err != nil {
		slog.Error("list users for admin", "error", err)
		http.Error(w, "Failed to load users", http.StatusInternalServerError)
		return
	}
	h.render(w, r, pages.AdminUsers(view.AdminUsersData{
		BasePage:   basePage(r, h.Services),
		Users:      users,
		Filter:     f,
		TotalCount: total,
		Page:       page,
		PerPage:    adminUsersPerPage,
	}))
}

// PageAdminUser serves GET /admin/users/{username}.
func (h *Handler) PageAdminUser(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFromContext(r.Context())
	detail, err := h.Services.AdminUser.Get(r.Context(), chi.URLParam(r, "username"))
	if errors.Is(err, sql.ErrNoRows) {
		h.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("load user for admin", "error", err)
		http.Error(w, "Failed to load user", http.StatusInternalServerError)
		return
	}
	// A reset link swapped into this page must not come back from the back-forward cache.
	w.Header().Set("Cache-Control", "no-store")
	h.render(w, r, pages.AdminUser(view.AdminUserData{
		BasePage: basePage(r, h.Services),
		Detail:   *detail,
		Self:     detail.User.ID == claims.UserID,
		Confirm:  h.confirmFactors(r, claims.UserID),
	}))
}

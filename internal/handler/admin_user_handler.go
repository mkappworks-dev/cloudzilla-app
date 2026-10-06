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
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
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
	h.render(w, r, pages.AdminUser(view.AdminUserData{
		BasePage: basePage(r, h.Services),
		Detail:   *detail,
		Self:     detail.User.ID == claims.UserID,
		Confirm:  h.confirmFactors(r, claims.UserID),
	}))
}

// adminUserAction confirms the admin's password, runs do on the account named
// in the URL, records action in the audit log and reloads the page.
func (h *Handler) adminUserAction(w http.ResponseWriter, r *http.Request, action string, do func(claims middleware.Claims, username string) (*model.User, map[string]any, error)) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	username := chi.URLParam(r, "username")
	u, meta, err := do(claims, username)
	if err != nil {
		status, msg := adminUserActionError(err)
		if status == http.StatusInternalServerError {
			slog.Error("admin user action", "action", action, "username", username, "error", err)
		}
		writeError(w, status, msg)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, action, model.AuditTargetUser, u.ID, u.Username, meta)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/users/"+u.Username, http.StatusSeeOther)
}

func adminUserActionError(err error) (int, string) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound, "No such user."
	case errors.Is(err, service.ErrAdminSelf):
		return http.StatusForbidden, "Change your own account under Account settings."
	case errors.Is(err, service.ErrLastSuperadmin):
		return http.StatusConflict, "This is the only active superadmin. Make another account a superadmin first."
	case errors.Is(err, service.ErrUserSuspended):
		return http.StatusConflict, "Unsuspend the account first."
	case errors.Is(err, service.ErrSoleOrgOwner):
		return http.StatusConflict, "This account is the only owner of an organization. Add another owner or delete the organization first."
	case errors.Is(err, service.ErrDeleteConfirmMismatch):
		return http.StatusBadRequest, "Type the username to confirm."
	}
	return http.StatusInternalServerError, "Couldn't change the account. Please try again."
}

// maxSuspendReason keeps a pasted essay out of the audit log.
const maxSuspendReason = 500

func (h *Handler) AdminSuspendUser(w http.ResponseWriter, r *http.Request) {
	h.adminUserAction(w, r, model.AuditActionAdminUserSuspend, func(c middleware.Claims, username string) (*model.User, map[string]any, error) {
		reason := strings.TrimSpace(r.FormValue("reason"))
		if len(reason) > maxSuspendReason {
			reason = reason[:maxSuspendReason]
		}
		u, err := h.Services.AdminUser.Suspend(r.Context(), c.UserID, username)
		return u, map[string]any{"reason": reason}, err
	})
}

func (h *Handler) AdminUnsuspendUser(w http.ResponseWriter, r *http.Request) {
	h.adminUserAction(w, r, model.AuditActionAdminUserUnsuspend, func(c middleware.Claims, username string) (*model.User, map[string]any, error) {
		u, err := h.Services.AdminUser.Unsuspend(r.Context(), c.UserID, username)
		return u, nil, err
	})
}

func (h *Handler) AdminPromoteUser(w http.ResponseWriter, r *http.Request) {
	h.adminUserAction(w, r, model.AuditActionAdminUserPromote, func(c middleware.Claims, username string) (*model.User, map[string]any, error) {
		u, err := h.Services.AdminUser.Promote(r.Context(), c.UserID, username)
		return u, nil, err
	})
}

func (h *Handler) AdminDemoteUser(w http.ResponseWriter, r *http.Request) {
	h.adminUserAction(w, r, model.AuditActionAdminUserDemote, func(c middleware.Claims, username string) (*model.User, map[string]any, error) {
		u, err := h.Services.AdminUser.Demote(r.Context(), c.UserID, username)
		return u, nil, err
	})
}

func (h *Handler) AdminResetUserTOTP(w http.ResponseWriter, r *http.Request) {
	h.adminUserAction(w, r, model.AuditActionAdminUser2FAReset, func(c middleware.Claims, username string) (*model.User, map[string]any, error) {
		u, err := h.Services.AdminUser.ResetTOTP(r.Context(), c.UserID, username)
		return u, nil, err
	})
}

func (h *Handler) AdminRevokeUserCredentials(w http.ResponseWriter, r *http.Request) {
	h.adminUserAction(w, r, model.AuditActionAdminUserCredentialsRevoke, func(c middleware.Claims, username string) (*model.User, map[string]any, error) {
		u, n, err := h.Services.AdminUser.RevokeCredentials(r.Context(), c.UserID, username)
		return u, map[string]any{"access_tokens": n.AccessTokens, "ssh_keys": n.SSHKeys, "oauth_authorizations": n.OAuthAuthorizations}, err
	})
}

// AdminDeleteUser answers with a redirect to the list, since the user page is gone.
func (h *Handler) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	username := chi.URLParam(r, "username")
	// Checked before the password so a typo doesn't spend a confirmation attempt.
	if strings.TrimSpace(r.FormValue("confirm_username")) != username {
		writeError(w, http.StatusBadRequest, "Type the username to confirm.")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	u, err := h.Services.AdminUser.Delete(r.Context(), claims.UserID, username, r.FormValue("confirm_username"))
	if err != nil {
		status, msg := adminUserActionError(err)
		if status == http.StatusInternalServerError {
			slog.Error("admin delete user", "username", username, "error", err)
		}
		writeError(w, status, msg)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionAdminUserDelete, model.AuditTargetUser, u.ID, u.Username,
		map[string]any{"email": u.Email})
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/admin/users")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

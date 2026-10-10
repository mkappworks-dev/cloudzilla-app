package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

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
	case errors.Is(err, service.ErrPasswordResetNoPassword):
		return http.StatusConflict, "This account signs in with Google or single sign-on, so it has no password to reset."
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

// AdminIssuePasswordResetLink answers with the link instead of reloading the
// page, since this response is the only place it is ever shown.
func (h *Handler) AdminIssuePasswordResetLink(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	username := chi.URLParam(r, "username")
	// Taken before issuing, so the shown expiry is never later than the stored one.
	expiresAt := time.Now().Add(service.PasswordResetManualTTL)
	u, link, err := h.Services.AdminUser.IssuePasswordResetLink(r.Context(), claims.UserID, username)
	if err != nil {
		status, msg := adminUserActionError(err)
		if status == http.StatusInternalServerError {
			slog.Error("admin issue password reset link", "username", username, "error", err)
		}
		writeError(w, status, msg)
		return
	}
	// A link that isn't in the audit log is never shown.
	if err := h.Services.AuditLog.RecordNow(r.Context(), r, claims.UserID, claims.Username, model.AuditActionPasswordResetLink,
		model.AuditTargetUser, u.ID, u.Username, map[string]any{"issued_by": model.PasswordResetByAdmin}); err != nil {
		slog.Error("admin issue password reset link: audit", "username", username, "error", err)
		writeError(w, http.StatusInternalServerError, "Couldn't record the link, so it wasn't shown. Please try again.")
		return
	}
	h.Services.AdminUser.NotifyPasswordResetLink(u.ID)
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("HX-Request") != "true" {
		writeJSON(w, http.StatusOK, map[string]any{"link": link, "expires_at": expiresAt.UTC().Format(time.RFC3339)})
		return
	}
	h.render(w, r, fragments.AdminResetLink(view.AdminResetLinkFragData{
		Username:  u.Username,
		Link:      link,
		ExpiresAt: expiresAt,
		Notified:  h.Services.Email.Enabled(),
	}))
}

// AdminAddOrgOwner makes the named user an owner of any org. The optional
// "from" username is the admin user page to return to.
func (h *Handler) AdminAddOrgOwner(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	orgName := chi.URLParam(r, "org")
	username := strings.TrimSpace(r.FormValue("username"))
	org, u, err := h.Services.Org.AdminAddOwner(r.Context(), orgName, username)
	if err != nil {
		status, msg := adminUserActionError(err)
		if status == http.StatusInternalServerError {
			slog.Error("admin add org owner", "org", orgName, "username", username, "error", err)
		}
		writeError(w, status, msg)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionAdminOrgOwnerAdd, model.AuditTargetOrg, org.ID, org.Name,
		map[string]any{"username": u.Username})
	back := "/admin/users/" + url.PathEscape(u.Username)
	if from := strings.TrimSpace(r.FormValue("from")); from != "" {
		back = "/admin/users/" + url.PathEscape(from)
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
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

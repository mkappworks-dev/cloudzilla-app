package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func confirmationFrom(r *http.Request) service.Confirmation {
	return service.Confirmation{Password: r.FormValue("password"), Code: r.FormValue("code"), EmailCode: r.FormValue("email_code")}
}

// reauthRefusal maps a failed confirmation to a status and a SettingsErrorMessage
// code, and reports whether err was one.
func reauthRefusal(userID int64, err error) (int, string, bool) {
	switch {
	case errors.Is(err, service.ErrReauthFailed):
		slog.Warn("re-authentication failed", "user_id", userID)
		return http.StatusForbidden, "reauth_failed", true
	case errors.Is(err, service.ErrReauthThrottled):
		slog.Warn("re-authentication throttled", "user_id", userID)
		return http.StatusTooManyRequests, "reauth_throttled", true
	case errors.Is(err, service.ErrReauthUnavailable):
		return http.StatusForbidden, "reauth_unavailable", true
	}
	return 0, "", false
}

// confirmFactors reports what userID confirms sensitive actions with. When that
// can't be read the form asks for both, since a factor the account lacks is ignored.
func (h *Handler) confirmFactors(ctx context.Context, userID int64) components.ConfirmFactors {
	f, err := h.Services.Reauth.Factors(ctx, userID)
	if err != nil {
		slog.Error("load confirmation factors", "user_id", userID, "error", err)
		return components.ConfirmFactors{Password: true, Code: true}
	}
	return components.ConfirmFactors{
		Password:    f.Password,
		Code:        f.Code,
		Email:       f.Email,
		Unavailable: !f.Password && !f.Code && !f.Email,
	}
}

// confirmGrant is confirmAction for administering a repository or organization.
// A personal access token skips it: creating the token took the password, and
// scripts can't answer a prompt. Tokens still confirm changes to the account itself.
func (h *Handler) confirmGrant(w http.ResponseWriter, r *http.Request, userID int64, c service.Confirmation, slot string) bool {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok && claims.PAT {
		return true
	}
	return h.confirmAction(w, r, userID, c, slot)
}

// confirmAction checks c before a sensitive action and answers the request when
// it fails. An HTMX form in a modal dialog names its error slot, where a toast
// would sit behind the backdrop; any other request gets a JSON error.
func (h *Handler) confirmAction(w http.ResponseWriter, r *http.Request, userID int64, c service.Confirmation, slot string) bool {
	_, err := h.Services.Reauth.Confirm(r.Context(), userID, c)
	if err == nil {
		return true
	}
	status, code, refused := reauthRefusal(userID, err)
	if !refused {
		slog.Error("confirm sensitive action", "user_id", userID, "error", err)
		status, code = http.StatusInternalServerError, "reauth_error"
	}
	if slot != "" && r.Header.Get("HX-Request") == "true" {
		renderFormError(w, slot, pages.SettingsErrorMessage(code))
	} else {
		writeError(w, status, pages.SettingsErrorMessage(code))
	}
	return false
}

// redirectReauthRefusal sends a settings form whose confirmation failed back to
// the section at anchor with the matching error.
func redirectReauthRefusal(w http.ResponseWriter, r *http.Request, userID int64, err error, anchor string) {
	_, code, refused := reauthRefusal(userID, err)
	if !refused {
		slog.Error("confirm sensitive action", "user_id", userID, "error", err)
		code = "reauth_error"
	}
	http.Redirect(w, r, "/settings?profile_error="+code+"#"+anchor, http.StatusSeeOther)
}

// SendConfirmCode handles POST /settings/confirm-code: it emails a one-time code
// to an account with no password or 2FA, for the confirm fields on any form.
func (h *Handler) SendConfirmCode(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	status, msg := http.StatusOK, "Code sent. It works once, for 10 minutes."
	switch err := h.Services.Reauth.SendEmailCode(r.Context(), claims.UserID); {
	case err == nil:
	case errors.Is(err, service.ErrEmailCodeCooldown):
		status, msg = http.StatusTooManyRequests, "A code was sent less than a minute ago. Check your email."
	case errors.Is(err, service.ErrEmailCodeNeedless):
		status, msg = http.StatusConflict, "This account confirms with its password or two-factor code."
	case errors.Is(err, service.ErrReauthUnavailable):
		status, msg = http.StatusConflict, pages.SettingsErrorMessage("reauth_unavailable")
	default:
		slog.Error("send confirmation code", "user_id", claims.UserID, "error", err)
		status, msg = http.StatusInternalServerError, "Couldn't send the code. Please try again."
	}
	// The status line swaps in whatever happened, so htmx always gets a 200.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(msg))
		return
	}
	if status != http.StatusOK {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// RevokeSessions handles POST /settings/sessions/revoke: it ends every session,
// then starts a new one for this browser. Tokens and OAuth grants are revoked separately.
func (h *Handler) RevokeSessions(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := h.Services.User.RevokeSessions(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("revoke sessions", "user_id", claims.UserID, "error", err)
		http.Redirect(w, r, "/settings?profile_error=sessions_revoke_failed#sessions", http.StatusSeeOther)
		return
	}
	h.setAuthCookie(w, token)
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionSessionsRevoke, model.AuditTargetUser, claims.UserID, claims.Username, nil)
	http.Redirect(w, r, "/settings?sessions_revoked=1#sessions", http.StatusSeeOther)
}

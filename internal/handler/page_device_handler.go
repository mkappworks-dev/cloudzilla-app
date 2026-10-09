package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const answeredMessage = "That request expired or was already answered. Run cz auth login again."

const (
	deviceCodeCookie = "cz_device_code"
	devicePath       = "/login/device"
)

// The typed code rides in a cookie, not a URL, so a provider sign-in can return to the confirm page.
func (h *Handler) setDeviceCookie(w http.ResponseWriter, userCode string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: deviceCodeCookie, Value: userCode, Path: devicePath, MaxAge: maxAge,
		HttpOnly: true, Secure: h.Cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func deviceFrameGuard(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
}

func formatUserCode(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// PageDeviceEntry handles GET /login/device. Any user_code in the query is ignored on purpose.
func (h *Handler) PageDeviceEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); !ok {
		http.Redirect(w, r, view.WithNext("/login", devicePath), http.StatusSeeOther)
		return
	}
	h.renderDeviceEntry(w, r, http.StatusOK, "")
}

func (h *Handler) renderDeviceEntry(w http.ResponseWriter, r *http.Request, status int, msg string) {
	deviceFrameGuard(w)
	h.setDeviceCookie(w, "", -1)
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	h.render(w, r, pages.DeviceEntry(view.DeviceEntryData{BasePage: basePage(r, h.Services), Error: msg}))
}

// DeviceLookup handles POST /login/device: the user types the code from their terminal.
func (h *Handler) DeviceLookup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	g, err := h.Services.DeviceGrant.Lookup(r.Context(), r.PostFormValue("user_code"))
	if errors.Is(err, service.ErrDeviceGrantNotFound) {
		h.renderDeviceEntry(w, r, http.StatusBadRequest, "That code isn't valid or has expired. Check your terminal and try again.")
		return
	}
	if err != nil {
		slog.Error("device login: look up code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.setDeviceCookie(w, g.UserCode, int(service.DeviceGrantTTL.Seconds()))
	http.Redirect(w, r, devicePath+"/confirm", http.StatusSeeOther)
}

// deviceGrantFromCookie returns the grant the user entered, or sends them back to the entry page.
func (h *Handler) deviceGrantFromCookie(w http.ResponseWriter, r *http.Request) (*model.DeviceGrant, bool) {
	c, err := r.Cookie(deviceCodeCookie)
	if err == nil {
		g, lerr := h.Services.DeviceGrant.Lookup(r.Context(), c.Value)
		if lerr == nil {
			return g, true
		}
		if !errors.Is(lerr, service.ErrDeviceGrantNotFound) {
			slog.Error("device login: look up code", "error", lerr)
		}
	}
	h.setDeviceCookie(w, "", -1)
	http.Redirect(w, r, devicePath, http.StatusSeeOther)
	return nil, false
}

func (h *Handler) renderDeviceConfirm(w http.ResponseWriter, r *http.Request, claims middleware.Claims, g *model.DeviceGrant, selected []string, status int, msg string) {
	deviceFrameGuard(w)
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	h.render(w, r, pages.DeviceConfirm(view.DeviceConfirmData{
		BasePage: basePage(r, h.Services), Username: claims.Username, UserCode: formatUserCode(g.UserCode),
		DeviceName: g.DeviceName, RequesterIP: g.RequesterIP, RequestedAt: view.Ago(g.CreatedAt),
		Scopes: g.Scopes, Selected: selected, Confirm: h.confirmFactors(r, claims.UserID), Error: msg,
	}))
}

// PageDeviceConfirm handles GET /login/device/confirm.
func (h *Handler) PageDeviceConfirm(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFromContext(r.Context())
	g, ok := h.deviceGrantFromCookie(w, r)
	if !ok {
		return
	}
	h.renderDeviceConfirm(w, r, claims, g, g.Scopes, http.StatusOK, "")
}

// DeviceApprove handles POST /login/device/approve.
func (h *Handler) DeviceApprove(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	g, ok := h.deviceGrantFromCookie(w, r)
	if !ok {
		return
	}
	details := func(scopes []string) map[string]any {
		return map[string]any{"device": g.DeviceName, "ip": g.RequesterIP, "scopes": scopes}
	}

	if r.PostFormValue("action") == "deny" {
		if err := h.Services.DeviceGrant.Deny(r.Context(), g.UserCode, claims.UserID); err != nil {
			if errors.Is(err, service.ErrDeviceGrantNotFound) {
				h.renderDeviceEntry(w, r, http.StatusGone, answeredMessage)
				return
			}
			slog.Error("device login: deny", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionDeviceDeny, model.AuditTargetUser, claims.UserID, claims.Username, details(nil))
		h.finishDevice(w, r, false)
		return
	}

	// Anything outside the request is dropped, so a forged scope (repo:admin included) can't widen it.
	var scopes []string
	for _, s := range r.PostForm["scope"] {
		if slices.Contains(g.Scopes, s) && !slices.Contains(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	if len(scopes) == 0 {
		h.renderDeviceConfirm(w, r, claims, g, g.Scopes, http.StatusBadRequest, "Keep at least one permission, or deny the request.")
		return
	}
	if _, err := h.Services.Reauth.Confirm(r.Context(), claims.UserID, confirmationFrom(r)); err != nil {
		status, code, refused := reauthRefusal(claims.UserID, err)
		if !refused {
			slog.Error("device login: confirm", "user_id", claims.UserID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.renderDeviceConfirm(w, r, claims, g, scopes, status, pages.SettingsErrorMessage(code))
		return
	}
	if err := h.Services.DeviceGrant.Approve(r.Context(), g.UserCode, claims.UserID, scopes); err != nil {
		if errors.Is(err, service.ErrDeviceGrantNotFound) {
			h.renderDeviceEntry(w, r, http.StatusGone, answeredMessage)
			return
		}
		slog.Error("device login: approve", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionDeviceApprove, model.AuditTargetUser, claims.UserID, claims.Username, details(scopes))
	h.Services.DeviceGrant.NotifyApproved(claims.UserID, g.DeviceName, g.RequesterIP, scopes)
	h.finishDevice(w, r, true)
}

func (h *Handler) finishDevice(w http.ResponseWriter, r *http.Request, approved bool) {
	deviceFrameGuard(w)
	h.setDeviceCookie(w, "", -1)
	h.render(w, r, pages.DeviceDone(view.DeviceDoneData{BasePage: basePage(r, h.Services), Approved: approved}))
}

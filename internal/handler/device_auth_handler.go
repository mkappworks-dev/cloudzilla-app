package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// deviceParams reads the body of a device endpoint: JSON or a URL-encoded form. The query
// string is never read, because a device code in a URL ends up in access logs.
func deviceParams(r *http.Request) (url.Values, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var m map[string]string
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			return nil, err
		}
		v := url.Values{}
		for k, val := range m {
			v.Set(k, val)
		}
		return v, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	return r.PostForm, nil
}

// DeviceCode handles POST /api/auth/device/code.
func (h *Handler) DeviceCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, err := deviceParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	dc, err := h.Services.DeviceGrant.Create(r.Context(), middleware.RemoteIP(r), strings.Fields(p.Get("scope")), p.Get("device_name"))
	switch {
	case errors.Is(err, service.ErrInvalidScope):
		writeError(w, http.StatusBadRequest, "invalid_scope")
	case errors.Is(err, service.ErrTooManyDeviceGrants):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too_many_requests")
	case err != nil:
		slog.Error("device code: create grant", "error", err)
		writeError(w, http.StatusInternalServerError, "server_error")
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"device_code":      dc.DeviceCode,
			"user_code":        dc.UserCode,
			"verification_uri": h.Cfg.Server.BaseURL + "/login/device",
			"expires_in":       dc.ExpiresIn,
			"interval":         dc.Interval,
		})
	}
}

// DeviceToken handles POST /api/auth/device/token.
func (h *Handler) DeviceToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, err := deviceParams(r)
	if err != nil || p.Get("grant_type") == "" || p.Get("device_code") == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if p.Get("grant_type") != deviceGrantType {
		writeError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	tok, err := h.Services.DeviceGrant.Poll(r.Context(), p.Get("device_code"))
	switch {
	case errors.Is(err, service.ErrAuthorizationPending), errors.Is(err, service.ErrSlowDown),
		errors.Is(err, service.ErrExpiredToken), errors.Is(err, service.ErrAccessDenied),
		errors.Is(err, service.ErrInvalidDeviceGrant):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		slog.Error("device token: poll", "error", err)
		writeError(w, http.StatusInternalServerError, "server_error")
	default:
		if u, err := h.Services.User.GetByID(r.Context(), tok.UserID); err != nil {
			slog.Error("device token: load user for audit", "user_id", tok.UserID, "error", err)
		} else {
			h.Services.AuditLog.Record(r.Context(), r, u.ID, u.Username, model.AuditActionTokenCreate, model.AuditTargetUser, u.ID, u.Username,
				map[string]any{"source": "device login", "token": tok.TokenName})
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"access_token": tok.AccessToken,
			"token_type":   "bearer",
			"scope":        strings.Join(tok.Scopes, " "),
		})
	}
}

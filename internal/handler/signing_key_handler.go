package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type signingKeyRequest struct {
	Title     string `json:"title"`
	PublicKey string `json:"public_key"`
	Password  string `json:"password"`
	Code      string `json:"code"`
	EmailCode string `json:"email_code"`
}

// readSigningKeyRequest decodes the body and runs the step-up; false means the response is written.
func (h *Handler) readSigningKeyRequest(w http.ResponseWriter, r *http.Request, claims middleware.Claims, needTitle bool) (signingKeyRequest, bool) {
	var req signingKeyRequest
	if isFormEncoded(r) {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return req, false
		}
		req = signingKeyRequest{
			Title: r.FormValue("title"), PublicKey: r.FormValue("public_key"), Password: r.FormValue("password"),
			Code: r.FormValue("code"), EmailCode: r.FormValue("email_code"),
		}
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return req, false
	}
	if req.PublicKey == "" || needTitle && req.Title == "" {
		msg := "public_key is required"
		if needTitle {
			msg = "title and public_key are required"
		}
		writeError(w, http.StatusBadRequest, msg)
		return req, false
	}
	if !h.confirmAction(w, r, claims.UserID, service.Confirmation{Password: req.Password, Code: req.Code, OneTimeCode: req.EmailCode}, "") {
		return req, false
	}
	return req, true
}

func signingKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrSigningKeyTaken):
		writeError(w, http.StatusConflict, "this key is already registered")
	case errors.Is(err, service.ErrGPGKeyExpired), errors.Is(err, service.ErrGPGKeyRevoked), errors.Is(err, service.ErrGPGNoSigningKey):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrInvalidGPGKey), errors.Is(err, service.ErrGPGPrivateKey), errors.Is(err, service.ErrInvalidSigningKey):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrSigningKeyNotFound):
		writeError(w, http.StatusNotFound, "key not found")
	default:
		writeError(w, http.StatusInternalServerError, "signing key request failed")
	}
}

func (h *Handler) recordSigningKey(r *http.Request, claims middleware.Claims, action string, meta map[string]any) {
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, action, model.AuditTargetUser, claims.UserID, claims.Username, meta)
}

func (h *Handler) AddGPGKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	req, ok := h.readSigningKeyRequest(w, r, claims, false)
	if !ok {
		return
	}
	key, err := h.Services.SigningKey.AddGPGKey(r.Context(), claims.UserID, req.PublicKey)
	if err != nil {
		signingKeyError(w, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionGPGKeyAdd, map[string]any{"fingerprint": key.Fingerprint})
	writeJSON(w, http.StatusCreated, key)
}

func (h *Handler) ListGPGKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	keys, err := h.Services.SigningKey.ListGPGKeys(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	if keys == nil {
		keys = []model.GPGKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) DeleteGPGKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}
	if err := h.Services.SigningKey.DeleteGPGKey(r.Context(), id, claims.UserID); err != nil {
		signingKeyError(w, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionGPGKeyDelete, map[string]any{"key_id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddSSHSigningKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	req, ok := h.readSigningKeyRequest(w, r, claims, true)
	if !ok {
		return
	}
	key, err := h.Services.SigningKey.AddSSHKey(r.Context(), claims.UserID, req.Title, req.PublicKey)
	if err != nil {
		signingKeyError(w, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionSSHSigningKeyAdd, map[string]any{"fingerprint": key.Fingerprint})
	writeJSON(w, http.StatusCreated, key)
}

func (h *Handler) ListSSHSigningKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	keys, err := h.Services.SigningKey.ListSSHKeys(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	if keys == nil {
		keys = []model.SSHSigningKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) DeleteSSHSigningKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}
	if err := h.Services.SigningKey.DeleteSSHKey(r.Context(), id, claims.UserID); err != nil {
		signingKeyError(w, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionSSHSigningKeyDelete, map[string]any{"key_id": id})
	w.WriteHeader(http.StatusNoContent)
}

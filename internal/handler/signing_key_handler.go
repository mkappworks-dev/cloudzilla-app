package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"golang.org/x/crypto/ssh"
)

const signingKeyErrorSlot = "#signing-key-error"

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
		signingKeyFormError(w, r, http.StatusBadRequest, msg)
		return req, false
	}
	if !h.confirmAction(w, r, claims.UserID, service.Confirmation{Password: req.Password, Code: req.Code, OneTimeCode: req.EmailCode}, signingKeyErrorSlot) {
		return req, false
	}
	return req, true
}

// signingKeyFormError answers an HTMX add with the message in the dialog's error slot, anything else with JSON.
func signingKeyFormError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if r.Header.Get("HX-Request") == "true" {
		renderFormError(w, signingKeyErrorSlot, msg)
		return
	}
	writeError(w, status, msg)
}

func signingKeyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrSigningKeyTaken):
		signingKeyFormError(w, r, http.StatusConflict, "this key is already registered")
	case errors.Is(err, service.ErrGPGKeyExpired), errors.Is(err, service.ErrGPGKeyRevoked), errors.Is(err, service.ErrGPGNoSigningKey):
		signingKeyFormError(w, r, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrInvalidGPGKey), errors.Is(err, service.ErrGPGPrivateKey), errors.Is(err, service.ErrInvalidSigningKey):
		signingKeyFormError(w, r, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrSigningKeyNotFound):
		writeError(w, http.StatusNotFound, "key not found")
	default:
		writeError(w, http.StatusInternalServerError, "signing key request failed")
	}
}

// signingKeysFragData loads what the signing keys list renders; the email and auth-key sets drive its badges.
func (h *Handler) signingKeysFragData(ctx context.Context, userID int64) (view.SigningKeysFragData, error) {
	var data view.SigningKeysFragData
	var err error
	if data.GPGKeys, err = h.Services.SigningKey.ListGPGKeys(ctx, userID); err != nil {
		return data, err
	}
	if data.SSHKeys, err = h.Services.SigningKey.ListSSHKeys(ctx, userID); err != nil {
		return data, err
	}
	authKeys, err := h.Services.SSHKey.ListByUser(ctx, userID)
	if err != nil {
		return data, err
	}
	data.AuthFingerprints = make(map[string]bool, len(authKeys))
	for _, k := range authKeys {
		// ssh_keys stores MD5 fingerprints; signing keys use SHA256.
		if pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(k.PublicKey)); err == nil {
			data.AuthFingerprints[ssh.FingerprintSHA256(pub)] = true
		}
	}
	user, err := h.Services.User.GetByID(ctx, userID)
	if err != nil {
		return data, err
	}
	extra, err := h.Services.EmailVerifier.Addresses(ctx, userID)
	if err != nil {
		return data, err
	}
	data.VerifiedEmails = map[string]bool{}
	if user.EmailVerified() {
		data.VerifiedEmails[strings.ToLower(user.Email)] = true
	}
	for _, e := range extra {
		if e.Verified() {
			data.VerifiedEmails[strings.ToLower(e.Email)] = true
		}
	}
	return data, nil
}

// renderSigningKeys answers an HTMX add or delete with the refreshed list.
func (h *Handler) renderSigningKeys(w http.ResponseWriter, r *http.Request, userID int64) {
	data, err := h.signingKeysFragData(r.Context(), userID)
	if err != nil {
		h.ServerError(w, r)
		return
	}
	h.render(w, r, fragments.SigningKeysList(data))
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
		signingKeyError(w, r, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionGPGKeyAdd, map[string]any{"fingerprint": key.Fingerprint})
	if r.Header.Get("HX-Request") == "true" {
		h.renderSigningKeys(w, r, claims.UserID)
		return
	}
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
		signingKeyError(w, r, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionGPGKeyDelete, map[string]any{"key_id": id})
	if r.Header.Get("HX-Request") == "true" {
		h.renderSigningKeys(w, r, claims.UserID)
		return
	}
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
		signingKeyError(w, r, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionSSHSigningKeyAdd, map[string]any{"fingerprint": key.Fingerprint})
	if r.Header.Get("HX-Request") == "true" {
		h.renderSigningKeys(w, r, claims.UserID)
		return
	}
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
		signingKeyError(w, r, err)
		return
	}
	h.recordSigningKey(r, claims, model.AuditActionSSHSigningKeyDelete, map[string]any{"key_id": id})
	if r.Header.Get("HX-Request") == "true" {
		h.renderSigningKeys(w, r, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

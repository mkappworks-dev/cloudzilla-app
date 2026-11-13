package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
		signingKeyFormError(w, r, http.StatusConflict, "this key is registered to another account on this instance; if it is yours, ask an administrator to remove it")
	case errors.Is(err, service.ErrSigningKeyOwned):
		signingKeyFormError(w, r, http.StatusConflict, "this key is already on your account")
	case errors.Is(err, service.ErrSigningKeyRevoked):
		signingKeyFormError(w, r, http.StatusConflict, err.Error())
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
	if data.GPGKeys, err = h.Services.SigningKey.ListGPGKeys(ctx, userID, true); err != nil {
		return data, err
	}
	if data.SSHKeys, err = h.Services.SigningKey.ListSSHKeys(ctx, userID, true); err != nil {
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
	key, created, err := h.Services.SigningKey.UpsertGPGKey(r.Context(), claims.UserID, req.PublicKey)
	if err != nil {
		signingKeyError(w, r, err)
		return
	}
	action, status := model.AuditActionGPGKeyAdd, http.StatusCreated
	if !created {
		action, status = model.AuditActionGPGKeyUpdate, http.StatusOK
	}
	h.recordSigningKey(r, claims, action, map[string]any{"fingerprint": key.Fingerprint})
	if r.Header.Get("HX-Request") == "true" {
		h.renderSigningKeys(w, r, claims.UserID)
		return
	}
	writeJSON(w, status, key)
}

func (h *Handler) ListGPGKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	keys, err := h.Services.SigningKey.ListGPGKeys(r.Context(), claims.UserID, includeRemoved(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	if keys == nil {
		keys = []model.GPGKey{}
	}
	writeJSON(w, http.StatusOK, keys)
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
	key, created, err := h.Services.SigningKey.UpsertSSHKey(r.Context(), claims.UserID, req.Title, req.PublicKey)
	if err != nil {
		signingKeyError(w, r, err)
		return
	}
	status := http.StatusCreated
	if created {
		h.recordSigningKey(r, claims, model.AuditActionSSHSigningKeyAdd, map[string]any{"fingerprint": key.Fingerprint})
	} else {
		status = http.StatusOK
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderSigningKeys(w, r, claims.UserID)
		return
	}
	writeJSON(w, status, key)
}

func (h *Handler) ListSSHSigningKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	keys, err := h.Services.SigningKey.ListSSHKeys(r.Context(), claims.UserID, includeRemoved(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	if keys == nil {
		keys = []model.SSHSigningKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

// includeRemoved is true for ?include=removed, the only way the JSON lists show removed keys.
func includeRemoved(r *http.Request) bool { return r.URL.Query().Get("include") == "removed" }

// signingKeyAction runs one change to a key of the signed-in user: Remove, Revoke or Restore. Restore re-arms a key,
// so it takes the add step-up.
func (h *Handler) signingKeyAction(w http.ResponseWriter, r *http.Request, action string, stepUp bool, do func(ctx context.Context, keyID, userID int64) error) {
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
	if stepUp && !h.readSigningKeyStepUp(w, r, claims) {
		return
	}
	if err := do(r.Context(), id, claims.UserID); err != nil {
		signingKeyError(w, r, err)
		return
	}
	h.recordSigningKey(r, claims, action, map[string]any{"key_id": id})
	if r.Header.Get("HX-Request") == "true" {
		h.renderSigningKeys(w, r, claims.UserID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readSigningKeyStepUp runs the add step-up for a request that carries no key; false means the response is written.
func (h *Handler) readSigningKeyStepUp(w http.ResponseWriter, r *http.Request, claims middleware.Claims) bool {
	var req signingKeyRequest
	if isFormEncoded(r) {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return false
		}
		req = signingKeyRequest{Password: r.FormValue("password"), Code: r.FormValue("code"), EmailCode: r.FormValue("email_code")}
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return h.confirmAction(w, r, claims.UserID, service.Confirmation{Password: req.Password, Code: req.Code, OneTimeCode: req.EmailCode}, signingKeyErrorSlot)
}

func (h *Handler) DeleteGPGKey(w http.ResponseWriter, r *http.Request) {
	h.signingKeyAction(w, r, model.AuditActionGPGKeyDelete, false, h.Services.SigningKey.RemoveGPGKey)
}

func (h *Handler) RevokeGPGKey(w http.ResponseWriter, r *http.Request) {
	h.signingKeyAction(w, r, model.AuditActionGPGKeyRevoke, false, h.Services.SigningKey.RevokeGPGKey)
}

func (h *Handler) RestoreGPGKey(w http.ResponseWriter, r *http.Request) {
	h.signingKeyAction(w, r, model.AuditActionGPGKeyRestore, true, h.Services.SigningKey.RestoreGPGKey)
}

func (h *Handler) DeleteSSHSigningKey(w http.ResponseWriter, r *http.Request) {
	h.signingKeyAction(w, r, model.AuditActionSSHSigningKeyDelete, false, h.Services.SigningKey.RemoveSSHKey)
}

func (h *Handler) RevokeSSHSigningKey(w http.ResponseWriter, r *http.Request) {
	h.signingKeyAction(w, r, model.AuditActionSSHSigningKeyRevoke, false, h.Services.SigningKey.RevokeSSHKey)
}

func (h *Handler) RestoreSSHSigningKey(w http.ResponseWriter, r *http.Request) {
	h.signingKeyAction(w, r, model.AuditActionSSHSigningKeyRestore, true, h.Services.SigningKey.RestoreSSHKey)
}

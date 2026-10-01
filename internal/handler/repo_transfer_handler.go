package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (h *Handler) ListRepoTransfers(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	transfers, err := h.Services.Repo.ListIncomingTransfers(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("repo transfers: list failed", "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list transfers")
		return
	}
	writeJSON(w, http.StatusOK, transfers)
}

// AcceptRepoTransfer takes the form field repo, the owner/name the recipient
// was shown, and refuses the transfer if the repository has moved since.
func (h *Handler) AcceptRepoTransfer(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := transferID(w, r)
	if !ok {
		return
	}
	offered := r.FormValue("repo")
	repo, err := h.Services.Repo.AcceptTransfer(r.Context(), id, claims.UserID, offered)
	switch {
	case errors.Is(err, service.ErrTransferNotFound):
		writeError(w, http.StatusNotFound, service.ErrTransferNotFound.Error())
		return
	case errors.Is(err, service.ErrRepoNameTaken):
		writeError(w, http.StatusConflict, "you already have a repository with that name; rename it, then accept again")
		return
	case errors.Is(err, service.ErrTransferChanged), errors.Is(err, service.ErrRepoChanged):
		writeError(w, http.StatusConflict, service.ErrTransferChanged.Error())
		return
	case err != nil:
		slog.Error("repo transfer: accept failed", "transfer_id", id, "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "transfer failed")
		return
	}

	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoTransfer, model.AuditTargetRepo, repo.ID, repo.Name, map[string]any{"from": offered})

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/"+repo.OwnerName+"/"+repo.Name)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (h *Handler) DeclineRepoTransfer(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := transferID(w, r)
	if !ok {
		return
	}
	t, err := h.Services.Repo.DeclineTransfer(r.Context(), id, claims.UserID)
	if errors.Is(err, service.ErrTransferNotFound) {
		writeError(w, http.StatusNotFound, service.ErrTransferNotFound.Error())
		return
	}
	if err != nil {
		slog.Error("repo transfer: decline failed", "transfer_id", id, "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to decline the transfer")
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoTransferDecline, model.AuditTargetRepo, t.RepoID, t.RepoName, map[string]any{"from": t.RequesterName})
	refreshOrNoContent(w, r)
}

func (h *Handler) CancelRepoTransfer(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, ok := h.readableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if !ok {
		return
	}
	if !h.Services.Repo.IsOwner(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	t, err := h.Services.Repo.CancelTransfer(r.Context(), repo, claims.UserID)
	if errors.Is(err, service.ErrTransferNotFound) {
		writeError(w, http.StatusNotFound, "no pending transfer")
		return
	}
	if err != nil {
		slog.Error("repo transfer: cancel failed", "repo_id", repo.ID, "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to cancel the transfer")
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoTransferCancel, model.AuditTargetRepo, repo.ID, repo.Name, map[string]any{"to": t.RecipientName})
	refreshOrNoContent(w, r)
}

func transferID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, service.ErrTransferNotFound.Error())
		return 0, false
	}
	return id, true
}

// refreshOrNoContent reloads an HTMX caller's page, which then shows the
// transfer gone.
func refreshOrNoContent(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
	}
	w.WriteHeader(http.StatusNoContent)
}

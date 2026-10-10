package handler

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageRepoTransfers lists the repositories offered to the viewer, each with
// the other collaborators, who keep their access if it is accepted.
func (h *Handler) PageRepoTransfers(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	transfers, err := h.Services.Repo.ListIncomingTransfers(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("repo transfers: list failed", "user_id", claims.UserID, "error", err)
		http.Error(w, "Failed to load repository transfers", http.StatusInternalServerError)
		return
	}
	offers := make([]view.IncomingRepoTransfer, 0, len(transfers))
	for _, t := range transfers {
		collabs, err := h.Services.Repo.ListCollaborators(r.Context(), t.RepoID)
		if err != nil {
			slog.Error("repo transfers: list collaborators failed", "repo_id", t.RepoID, "error", err)
			http.Error(w, "Failed to load repository transfers", http.StatusInternalServerError)
			return
		}
		others := slices.DeleteFunc(collabs, func(p model.Permission) bool { return p.UserID == claims.UserID })
		offers = append(offers, view.IncomingRepoTransfer{RepoTransfer: t, Collaborators: others})
	}
	h.render(w, r, pages.RepoTransfers(view.RepoTransfersData{
		BasePage:  basePage(r, h.Services),
		Transfers: offers,
	}))
}

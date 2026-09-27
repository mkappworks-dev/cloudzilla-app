package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageInvite(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.usableInvitation(w, r)
	if !ok {
		return
	}

	h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv}))
}

func (h *Handler) PageInviteSubmit(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.usableInvitation(w, r)
	if !ok {
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv, Error: "All fields are required"}))
		return
	}
	if msg := passwordLengthMessage(password); msg != "" {
		h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv, Error: msg}))
		return
	}

	// Create the user (bypasses allow_registration)
	if _, err := h.Services.User.CreateFromInvitation(r.Context(), inv, username, password); err != nil {
		if errors.Is(err, service.ErrInvitationUnusable) {
			h.renderInvalidInvitation(w, r)
			return
		}
		logCreateAccountFailure(r.Context(), "invite: create user failed", err, "invitation_id", inv.ID)
		h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv, Error: createAccountErrorMessage(err)}))
		return
	}

	// Authenticate and set cookie (bypasses allow_login)
	_, jwtToken, err := h.Services.User.Authenticate(r.Context(), inv.Email, password)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.setAuthCookie(w, jwtToken)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// usableInvitation writes the response itself when it returns false.
func (h *Handler) usableInvitation(w http.ResponseWriter, r *http.Request) (*model.Invitation, bool) {
	inv, err := h.Services.Invitation.GetUsable(r.Context(), chi.URLParam(r, "token"))
	return inv, h.linkLookupOK(w, r, err, service.ErrInvitationUnusable, h.renderInvalidInvitation, "invite")
}

// The invitation is withheld: its email may belong to a registered account,
// and the link is unauthenticated.
func (h *Handler) renderInvalidInvitation(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services)}))
}

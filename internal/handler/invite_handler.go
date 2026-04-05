package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageInvite(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	inv, err := h.Services.Invitation.GetByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "invitation not found", http.StatusNotFound)
		return
	}

	if err := h.Services.Invitation.Validate(inv); err != nil {
		h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv, Error: err.Error()}))
		return
	}

	h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv}))
}

func (h *Handler) PageInviteSubmit(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	inv, err := h.Services.Invitation.GetByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "invitation not found", http.StatusNotFound)
		return
	}

	if err := h.Services.Invitation.Validate(inv); err != nil {
		h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv, Error: err.Error()}))
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv, Error: "All fields are required"}))
		return
	}

	// Create the user (bypasses allow_registration)
	user, err := h.Services.User.Create(r.Context(), username, inv.Email, password)
	if err != nil {
		h.render(w, r, pages.Invite(view.InviteData{BasePage: basePage(r, h.Services), Invitation: inv, Error: "Failed to create account: " + err.Error()}))
		return
	}

	// Mark user as invited so they can always log in
	// We need store access via a service method or expose MarkInvited via UserService
	if err := h.Services.User.MarkInvited(r.Context(), user.ID); err != nil {
		// Non-fatal — log but continue
		_ = err
	}

	// Accept the invitation
	if err := h.Services.Invitation.Accept(r.Context(), inv.ID); err != nil {
		_ = err
	}

	// Authenticate and set cookie (bypasses allow_login)
	_, jwtToken, err := h.Services.User.Authenticate(r.Context(), inv.Email, password)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    jwtToken,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// requestSignup answers every valid address with the same page and mails in
// the background, so neither the response nor its timing shows whether the
// address has an account.
func (h *Handler) requestSignup(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if !validEmail(email) {
		h.render(w, r, pages.RegisterEmail(view.RegisterEmailData{BasePage: basePage(r, h.Services), Email: email, Error: "Enter a valid email address"}))
		return
	}
	concurrency.Go("signup.request", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := h.Services.Signup.Request(ctx, email); err != nil {
			slog.Error("signup: request failed", "error", err)
		}
	})
	h.render(w, r, pages.RegisterCheckInbox(view.RegisterCheckInboxData{BasePage: basePage(r, h.Services)}))
}

// RFC 5321 caps a path at 256 bytes, angle brackets included.
const maxEmailBytes = 254

// validEmail accepts a bare address only; refusing display names and CR/LF
// also keeps it safe to put in the To header.
func validEmail(s string) bool {
	if len(s) > maxEmailBytes {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s
}

func (h *Handler) PageRegisterComplete(w http.ResponseWriter, r *http.Request) {
	if !h.registrationOpen(w, r) {
		return
	}
	signup, ok := h.usableSignup(w, r)
	if !ok {
		return
	}
	h.render(w, r, pages.RegisterComplete(view.RegisterCompleteData{BasePage: basePage(r, h.Services), Signup: signup}))
}

func (h *Handler) PageRegisterCompleteSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.registrationOpen(w, r) {
		return
	}
	signup, ok := h.usableSignup(w, r)
	if !ok {
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	renderError := func(msg string) {
		h.render(w, r, pages.RegisterComplete(view.RegisterCompleteData{BasePage: basePage(r, h.Services), Signup: signup, Username: username, Error: msg}))
	}
	if username == "" || password == "" {
		renderError("All fields are required")
		return
	}
	if len(password) < 8 {
		renderError("Password must be at least 8 characters")
		return
	}
	if msg := passwordLengthMessage(password); msg != "" {
		renderError(msg)
		return
	}

	user, err := h.Services.Signup.Complete(r.Context(), chi.URLParam(r, "token"), username, password)
	if errors.Is(err, service.ErrSignupTokenUnusable) {
		h.renderInvalidSignupLink(w, r)
		return
	}
	if err != nil {
		logCreateAccountFailure(r.Context(), "signup: create user failed", err)
		renderError(createAccountErrorMessage(err))
		return
	}

	token, err := h.Services.User.GenerateTokenForUser(r.Context(), user.ID)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.setAuthCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// registrationOpen writes the redirect itself when it returns false. It also
// gates outstanding links, so closing registration stops them too.
func (h *Handler) registrationOpen(w http.ResponseWriter, r *http.Request) bool {
	if !h.Services.SiteSetting.AllowRegistration(r.Context()) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return false
	}
	return true
}

// usableSignup writes the response itself when it returns false.
func (h *Handler) usableSignup(w http.ResponseWriter, r *http.Request) (*model.SignupToken, bool) {
	signup, err := h.Services.Signup.GetUsable(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, service.ErrSignupTokenUnusable) {
		h.renderInvalidSignupLink(w, r)
		return nil, false
	}
	if err != nil {
		slog.Error("signup: link lookup failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	return signup, true
}

func (h *Handler) renderInvalidSignupLink(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.RegisterComplete(view.RegisterCompleteData{BasePage: basePage(r, h.Services)}))
}

func (h *Handler) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})
}

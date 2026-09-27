package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
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

// validEmail accepts a bare address only; refusing display names and CR/LF
// also keeps it safe to put in the To header.
func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s
}

func (h *Handler) PageRegisterComplete(w http.ResponseWriter, r *http.Request)       {}
func (h *Handler) PageRegisterCompleteSubmit(w http.ResponseWriter, r *http.Request) {}

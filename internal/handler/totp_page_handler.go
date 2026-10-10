package handler

import (
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageTOTPVerify renders GET /auth/2fa — the 6-digit input page.
func (h *Handler) PageTOTPVerify(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	if _, err := r.Cookie(totpPendingCookieName); err != nil {
		http.Redirect(w, r, view.WithNext("/login", next), http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.TOTPVerify(view.TOTPVerifyPageData{BasePage: basePage(r, h.Services), Next: next}))
}

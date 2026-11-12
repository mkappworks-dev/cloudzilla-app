package handler

import (
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageTerms handles GET /terms, open to signed-out visitors so the sign-up and organization forms can link it.
func (h *Handler) PageTerms(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.Terms(view.TermsData{
		BasePage: basePage(r, h.Services),
		HTML:     markdown.Render(h.Services.SiteSetting.Terms(r.Context())),
	}))
}

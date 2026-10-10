package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageImportRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	orgs, err := h.Services.Org.ListOwnedByUser(ctx, claims.UserID)
	if err != nil {
		slog.Error("list owned orgs", "error", err)
		orgs = []model.Organization{}
	}
	q := r.URL.Query()
	// As on /repos/new, ?owner= is honored only for an org the viewer owns.
	owner, private := claims.Username, false
	for _, o := range orgs {
		if o.Name == q.Get("owner") {
			owner, private = o.Name, o.DefaultRepoVisibility != "public"
			break
		}
	}
	h.render(w, r, pages.RepoImport(view.RepoImportData{
		BasePage:        withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(ctx, claims.UserID)),
		OwnedOrgs:       orgs,
		DefaultOwner:    owner,
		DefaultPrivate:  private,
		DefaultURL:      q.Get("url"),
		DefaultName:     q.Get("name"),
		MirrorIntervals: h.mirrorIntervalOptions(),
	}))
}

func (h *Handler) mirrorIntervalOptions() []view.IntervalOption {
	if !h.Services.Mirror.Enabled() {
		return nil
	}
	var opts []view.IntervalOption
	for _, c := range h.Services.Mirror.IntervalChoices() {
		opts = append(opts, view.IntervalOption{Value: c.Value, Label: c.Label, Selected: c.Default})
	}
	return opts
}

func (h *Handler) PageImportStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	job, err := h.Services.Import.Get(claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		h.NotFound(w, r)
		return
	}
	repoURL := "/" + job.Owner + "/" + job.Name
	if r.Header.Get("HX-Request") == "true" {
		if job.Status == service.ImportDone {
			w.Header().Set("HX-Redirect", repoURL)
		}
		h.render(w, r, pages.RepoImportStatusPanel(job))
		return
	}
	if job.Status == service.ImportDone {
		http.Redirect(w, r, repoURL, http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.RepoImportStatus(view.RepoImportStatusData{
		BasePage: withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(r.Context(), claims.UserID)),
		Job:      job,
	}))
}

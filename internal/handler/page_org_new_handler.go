package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageNewOrganization renders the form for creating a new organization.
//
// Renders with the bare global header — no AccountSubnav. Orgs aren't a
// subnav-level concept (no "Organizations" tab exists), and dropping the
// chrome keeps the form focused on the single task.
func (h *Handler) PageNewOrganization(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	h.render(w, r, pages.NewOrganization(view.NewOrganizationData{
		BasePage: basePage(r, h.Services),
	}))
}

// CreateOrganization handles the new-organization form POST.
func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderNewOrgError(w, r, "", "", "Invalid form submission.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))

	// TOS must be accepted — enforced server-side, not just client-side.
	if r.FormValue("accept_tos") != "on" {
		h.renderNewOrgError(w, r, name, description, "You must accept the terms of service.")
		return
	}
	// Name must be filesystem/URL-safe.
	if err := service.ValidateName(name); err != nil {
		h.renderNewOrgError(w, r, name, description, "Invalid organization name: "+err.Error())
		return
	}
	org, err := h.Services.Org.Create(r.Context(), claims.UserID, name, name, description)
	if err != nil {
		msg := "Could not create the organization. Please try again."
		if errors.Is(err, service.ErrOrgNameTaken) {
			msg = "That organization name is already taken."
		}
		h.renderNewOrgError(w, r, name, description, msg)
		return
	}
	http.Redirect(w, r, "/"+org.Name, http.StatusSeeOther)
}

func (h *Handler) renderNewOrgError(w http.ResponseWriter, r *http.Request, name, description, msg string) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	h.render(w, r, pages.NewOrganization(view.NewOrganizationData{
		BasePage:    basePage(r, h.Services),
		Error:       msg,
		Name:        name,
		Description: description,
	}))
}

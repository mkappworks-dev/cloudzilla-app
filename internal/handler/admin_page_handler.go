package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageAdminSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	settings, _ := h.Services.SiteSetting.GetAll(r.Context())
	if settings == nil {
		settings = []model.SiteSetting{}
	}

	invitations, _ := h.Services.Invitation.List(r.Context())
	if invitations == nil {
		invitations = []model.Invitation{}
	}

	signingKey, err := h.signingKeyData(r.Context())
	if err != nil {
		slog.Error("admin settings: instance signing key", "error", err)
	}

	h.render(w, r, pages.AdminSettings(view.AdminSettingsData{
		BasePage:    basePage(r, h.Services),
		Settings:    settings,
		Invitations: invitations,
		Terms:       h.Services.SiteSetting.Terms(r.Context()),
		Confirm:     h.confirmFactors(r, claims.UserID),
		SigningKey:  signingKey,
	}))
}

// signingKeyData reads the instance signing key for the admin page. On a failed read the section still renders, as
// if no key existed.
func (h *Handler) signingKeyData(ctx context.Context) (view.AdminSigningKeyFragData, error) {
	st, err := h.Services.InstanceSigner.State(ctx)
	if err != nil {
		return view.AdminSigningKeyFragData{Sealable: h.Services.Secrets != nil}, err
	}
	data := view.AdminSigningKeyFragData{Sealable: st.Sealable, Unreadable: st.Unreadable}
	if st.Key != nil {
		data.HasKey, data.Fingerprint, data.PublicKey, data.CreatedAt = true, st.Key.Fingerprint, st.Key.PublicKey, st.Key.CreatedAt
	}
	return data, nil
}

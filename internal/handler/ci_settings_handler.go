package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

const secretFormError = "#secret-form-error"

// settingsCI is nil when ci.enabled is off, which hides the CI/CD section.
func (h *Handler) settingsCI(ctx context.Context, repo *model.Repository) *view.CISettings {
	if !h.Cfg.CI.Enabled {
		return nil
	}
	secrets, err := h.Services.CISecret.List(ctx, repo.ID)
	if err != nil {
		slog.Warn("settings: secrets unavailable", "repo_id", repo.ID, "error", err)
	}
	on, err := h.Services.WorkflowRun.RepoCIEnabled(ctx, repo.ID)
	if err != nil {
		on = true
	}
	return &view.CISettings{
		Enabled: on, Secrets: secrets, HasKey: h.Services.CISecret.HasKey(),
		Base: "/" + repo.OwnerName + "/" + repo.Name + "/settings", ActionsURL: actionsBase(repo),
	}
}

// UpdateRepoCI handles POST /{owner}/{repo}/settings/ci: the repo's CI switch.
func (h *Handler) UpdateRepoCI(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.manageableRepo(w, r)
	if !ok {
		return
	}
	if err := h.Services.WorkflowRun.SetRepoEnabled(r.Context(), repo.ID, r.FormValue("ci_enabled") == "on"); err != nil {
		slog.Error("settings: set ci enabled failed", "repo_id", repo.ID, "error", err)
		settingsError(w, r, http.StatusInternalServerError, "failed to update settings")
		return
	}
	redirectAfterSave(w, r, "/"+repo.OwnerName+"/"+repo.Name+"/settings#ci")
}

func (h *Handler) secretsCard(w http.ResponseWriter, r *http.Request, repo *model.Repository) {
	ci := h.settingsCI(r.Context(), repo)
	if ci == nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, fragments.CISecretsCard(*ci))
}

// SaveCISecret handles POST /{owner}/{repo}/settings/secrets: add a secret or replace its value. The value is never
// sent back, not even on an error.
func (h *Handler) SaveCISecret(w http.ResponseWriter, r *http.Request) {
	repo, userID, ok := h.manageableRepo(w, r)
	if !ok {
		return
	}
	err := h.Services.CISecret.Save(r.Context(), repo, userID, r.FormValue("name"), r.FormValue("value"))
	switch {
	case err == nil:
		h.secretsCard(w, r, repo)
	case errors.Is(err, service.ErrCISecretNoKey), errors.Is(err, service.ErrCISecretName), errors.Is(err, service.ErrCISecretValue),
		errors.Is(err, service.ErrCISecretLimit):
		renderFormError(w, secretFormError, err.Error())
	case errors.Is(err, service.ErrCISecretForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		slog.Error("settings: save ci secret failed", "repo_id", repo.ID, "error", err)
		renderFormError(w, secretFormError, "That did not work. Try again.")
	}
}

// DeleteCISecret handles POST /{owner}/{repo}/settings/secrets/{name}/delete.
func (h *Handler) DeleteCISecret(w http.ResponseWriter, r *http.Request) {
	repo, userID, ok := h.manageableRepo(w, r)
	if !ok {
		return
	}
	switch err := h.Services.CISecret.Delete(r.Context(), repo, userID, chi.URLParam(r, "name")); {
	case err == nil:
		h.secretsCard(w, r, repo)
	case errors.Is(err, service.ErrCISecretNotFound):
		http.NotFound(w, r)
	default:
		slog.Error("settings: delete ci secret failed", "repo_id", repo.ID, "error", err)
		settingsError(w, r, http.StatusInternalServerError, "failed to delete the secret")
	}
}

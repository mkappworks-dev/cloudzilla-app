package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const maxProfileReadmeBytes = 200 * 1024

// UpdateProfileReadme handles POST /settings/profile-readme — the inline
// editor on the user's own profile page.
func (h *Handler) UpdateProfileReadme(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, err := h.Services.User.GetByID(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), user.Username, user.Username)
	if err != nil || repo == nil {
		http.Redirect(w, r, "/repos/new?name="+url.QueryEscape(user.Username)+"&visibility=public&init_readme=1", http.StatusSeeOther)
		return
	}
	if repo.Private {
		redirectReadmeError(w, r, user.Username, "profile_repo_private")
		return
	}

	content := r.FormValue("content")
	if len(content) > maxProfileReadmeBytes {
		redirectReadmeError(w, r, user.Username, "too_large")
		return
	}

	message := strings.TrimSpace(r.FormValue("message"))

	authorName := user.Name
	if strings.TrimSpace(authorName) == "" {
		authorName = user.Username
	}
	authorEmail := user.Email
	if authorEmail == "" {
		authorEmail = user.Username + "@noreply.cloudzilla"
	}

	if err := h.Services.Code.SaveProfileReadme(user.Username, user.Username, repo.DefaultBranch, content, authorName, authorEmail, message); err != nil {
		if errors.Is(err, service.ErrProfileRepoMissing) {
			http.Redirect(w, r, "/repos/new?name="+url.QueryEscape(user.Username)+"&visibility=public&init_readme=1", http.StatusSeeOther)
			return
		}
		slog.Error("failed to save profile README", "username", user.Username, "error", err)
		redirectReadmeError(w, r, user.Username, "save_failed")
		return
	}

	http.Redirect(w, r, "/"+user.Username, http.StatusSeeOther)
}

func redirectReadmeError(w http.ResponseWriter, r *http.Request, username, code string) {
	http.Redirect(w, r, "/"+username+"?readme_error="+url.QueryEscape(code), http.StatusSeeOther)
}

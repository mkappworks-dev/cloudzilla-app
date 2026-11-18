package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type containerTagJSON struct {
	Name      string    `json:"name"`
	Digest    string    `json:"digest"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	Platforms []string  `json:"platforms"`
	Publisher string    `json:"publisher"`
	PullCount int64     `json:"pull_count"`
	UpdatedAt time.Time `json:"updated_at"`
}

type untaggedManifestJSON struct {
	Digest       string    `json:"digest"`
	MediaType    string    `json:"media_type"`
	Size         int64     `json:"size"`
	ArtifactType string    `json:"artifact_type,omitempty"`
	Platforms    []string  `json:"platforms"`
	PullCount    int64     `json:"pull_count"`
	CreatedAt    time.Time `json:"created_at"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// GetContainerPackage handles GET /api/repos/{owner}/{repo}/packages/container/{name...}.
func (h *Handler) GetContainerPackage(w http.ResponseWriter, r *http.Request) {
	gu, ok := h.packageCaller(w, r)
	if !ok {
		return
	}
	repo, ok := h.packageRepo(w, r, gu)
	if !ok {
		return
	}
	var viewer *int64
	if gu != nil {
		viewer = &gu.ID
	}
	d, err := h.Services.Package.ContainerDetail(r.Context(), repo.OwnerName, chi.URLParam(r, "*"), viewer)
	if err == nil && (d.Package.RepoID == nil || *d.Package.RepoID != repo.ID) {
		err = service.ErrPackageNotFound
	}
	if errors.Is(err, service.ErrPackageNotFound) {
		writeError(w, http.StatusNotFound, "package not found")
		return
	}
	if err != nil {
		slog.Error("get container package failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	tags := make([]containerTagJSON, len(d.Tags))
	for i, t := range d.Tags {
		tags[i] = containerTagJSON{Name: t.Name, Digest: t.Digest, MediaType: t.MediaType, Size: t.Size,
			Platforms: nonNil(t.Platforms), Publisher: t.Publisher, PullCount: t.PullCount, UpdatedAt: t.UpdatedAt}
	}
	untagged := make([]untaggedManifestJSON, len(d.Untagged))
	for i, m := range d.Untagged {
		untagged[i] = untaggedManifestJSON{Digest: m.Digest, MediaType: m.MediaType, Size: m.Size,
			ArtifactType: m.ArtifactType, Platforms: nonNil(m.Platforms), PullCount: m.PullCount, CreatedAt: m.CreatedAt}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": d.Package.Name, "type": d.Package.Type, "created_at": d.Package.CreatedAt, "updated_at": d.Package.UpdatedAt,
		"tags": tags, "untagged": untagged,
	})
}

// readablePackageRepo is packageRepo for a caller who must be signed in: 401 to nobody, 404 to someone who can't
// read the repository. Whether they may change the package is the service's rule, not the handler's.
func (h *Handler) readablePackageRepo(w http.ResponseWriter, r *http.Request) (*model.Repository, *gitUser, bool) {
	gu, ok := h.packageCaller(w, r)
	if !ok || !requirePackageCaller(w, gu) {
		return nil, nil, false
	}
	repo, ok := h.packageRepo(w, r, gu)
	if !ok {
		return nil, nil, false
	}
	return repo, gu, true
}

// linkedPackageID finds the package of typ and name linked to repo, writing a 404 when there is none.
func (h *Handler) linkedPackageID(w http.ResponseWriter, r *http.Request, repo *model.Repository, typ, name string) (int64, bool) {
	id, err := h.Services.Package.LinkedPackageID(r.Context(), repo, typ, name)
	if errors.Is(err, service.ErrPackageNotFound) {
		writeError(w, http.StatusNotFound, "package not found")
		return 0, false
	}
	if err != nil {
		slog.Error("find package failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return 0, false
	}
	return id, true
}

func (h *Handler) finishPackageChange(w http.ResponseWriter, repo *model.Repository, err error) {
	switch {
	case errors.Is(err, service.ErrPackageForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, service.ErrRelinkTarget):
		writeError(w, http.StatusBadRequest, "A package can only be linked to a repository of its path owner.")
	case errors.Is(err, service.ErrPackageNotFound):
		writeError(w, http.StatusNotFound, "package not found")
	case err != nil:
		slog.Error("change package failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteContainerPackage handles DELETE /api/repos/{owner}/{repo}/packages/container/{name...}.
func (h *Handler) DeleteContainerPackage(w http.ResponseWriter, r *http.Request) {
	h.deleteWholePackage(w, r, model.PackageTypeContainer, chi.URLParam(r, "*"))
}

// DeleteGenericPackage handles DELETE /api/repos/{owner}/{repo}/packages/generic/{name}.
func (h *Handler) DeleteGenericPackage(w http.ResponseWriter, r *http.Request) {
	h.deleteWholePackage(w, r, model.PackageTypeGeneric, chi.URLParam(r, "name"))
}

func (h *Handler) deleteWholePackage(w http.ResponseWriter, r *http.Request, typ, name string) {
	repo, gu, ok := h.readablePackageRepo(w, r)
	if !ok {
		return
	}
	id, ok := h.linkedPackageID(w, r, repo, typ, name)
	if !ok {
		return
	}
	h.finishPackageChange(w, repo, h.Services.Package.Delete(r.Context(), gu.ID, id))
}

// RelinkPackage handles POST /api/repos/{owner}/{repo}/packages/relink; the body names the package and the
// "owner/repo" to move it to.
func (h *Handler) RelinkPackage(w http.ResponseWriter, r *http.Request) {
	repo, gu, ok := h.readablePackageRepo(w, r)
	if !ok {
		return
	}
	var body struct{ Type, Name, Repo string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	owner, name, found := strings.Cut(body.Repo, "/")
	if !found || (body.Type != model.PackageTypeContainer && body.Type != model.PackageTypeGeneric) {
		writeError(w, http.StatusBadRequest, "type must be container or generic and repo must be owner/repo")
		return
	}
	id, ok := h.linkedPackageID(w, r, repo, body.Type, body.Name)
	if !ok {
		return
	}
	target, err := h.Services.Repo.Get(r.Context(), owner, name)
	if err != nil || !h.Services.Repo.CanRead(r.Context(), target, &gu.ID) {
		writeError(w, http.StatusBadRequest, "The target repository was not found.")
		return
	}
	h.finishPackageChange(w, repo, h.Services.Package.Relink(r.Context(), gu.ID, id, target.ID))
}

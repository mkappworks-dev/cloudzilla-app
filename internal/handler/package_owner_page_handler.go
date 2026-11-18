package handler

import (
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

// ownerPackagesData is the Packages tab of the profile of ownerName, a user or an organization. The path owner also
// gets the packages sitting in Repositories they can't read, so they can delete or re-link them.
func (h *Handler) ownerPackagesData(r *http.Request, ownerName string) (view.OwnerPackagesData, error) {
	viewer := viewerOf(r)
	all, err := h.Services.Package.ListByOwner(r.Context(), ownerName, viewer)
	if err != nil {
		return view.OwnerPackagesData{}, err
	}
	data := view.OwnerPackagesData{
		Owner:       ownerName,
		Total:       len(all),
		Type:        r.URL.Query().Get("type"),
		Repo:        r.URL.Query().Get("repo"),
		RepoOptions: view.OwnerPackageRepos(all),
	}
	data.Packages = view.FilterOwnerPackages(all, data.Type, data.Repo)
	if viewer != nil && h.Services.Package.IsPathOwner(r.Context(), ownerName, *viewer) {
		data.CanManage = true
		repos, err := h.Services.Repo.ListByOwnerVisibleTo(r.Context(), ownerName, viewer)
		if err != nil {
			return data, err
		}
		for _, repo := range repos {
			data.Targets = append(data.Targets, repo.Name)
		}
	}
	return data, nil
}
